/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package service

import (
	"sync"

	corecomm "github.com/hyperledger/fabric/core/comm"
	"github.com/hyperledger/fabric/core/committer"
	"github.com/hyperledger/fabric/core/committer/txvalidator"
	"github.com/hyperledger/fabric/core/common/privdata"
	"github.com/hyperledger/fabric/core/deliverservice"
	"github.com/hyperledger/fabric/core/deliverservice/blocksprovider"
	"github.com/hyperledger/fabric/gossip/api"
	"github.com/hyperledger/fabric/gossip/comm"
	"github.com/hyperledger/fabric/gossip/common"
	gossipcommon "github.com/hyperledger/fabric/gossip/common"
	"github.com/hyperledger/fabric/gossip/discovery"
	"github.com/hyperledger/fabric/gossip/election"
	"github.com/hyperledger/fabric/gossip/filter"
	"github.com/hyperledger/fabric/gossip/gossip"
	gossipmetrics "github.com/hyperledger/fabric/gossip/metrics"
	gossipprivdata "github.com/hyperledger/fabric/gossip/privdata"
	"github.com/hyperledger/fabric/gossip/protoext"
	"github.com/hyperledger/fabric/gossip/state"
	"github.com/hyperledger/fabric/gossip/util"
	"github.com/hyperledger/fabric/internal/pkg/identity"
	gproto "github.com/hyperledger/fabric/protos/gossip"
	"github.com/hyperledger/fabric/protos/transientstore"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/pkg/errors"
	"google.golang.org/grpc"
)

// gossipSvc is the interface of the gossip component.
type gossipSvc interface {
	// SelfMembershipInfo returns the peer's membership information
	SelfMembershipInfo() discovery.NetworkMember

	// SelfChannelInfo returns the peer's latest StateInfo message of a given channel
	SelfChannelInfo(common.ChannelID) *protoext.SignedGossipMessage

	// Send sends a message to remote peers
	Send(msg *gproto.GossipMessage, peers ...*comm.RemotePeer)

	// SendByCriteria sends a given message to all peers that match the given SendCriteria
	SendByCriteria(*protoext.SignedGossipMessage, gossip.SendCriteria) error

	// GetPeers returns the NetworkMembers considered alive
	Peers() []discovery.NetworkMember

	// PeersOfChannel returns the NetworkMembers considered alive
	// and also subscribed to the channel given
	PeersOfChannel(common.ChannelID) []discovery.NetworkMember

	// UpdateMetadata updates the self metadata of the discovery layer
	// the peer publishes to other peers
	UpdateMetadata(metadata []byte)

	// UpdateLedgerHeight updates the ledger height the peer
	// publishes to other peers in the channel
	UpdateLedgerHeight(height uint64, channelID common.ChannelID)

	// UpdateChaincodes updates the chaincodes the peer publishes
	// to other peers in the channel
	UpdateChaincodes(chaincode []*gproto.Chaincode, channelID common.ChannelID)

	// Gossip sends a message to other peers to the network
	Gossip(msg *gproto.GossipMessage)

	// PeerFilter receives a SubChannelSelectionCriteria and returns a RoutingFilter that selects
	// only peer identities that match the given criteria, and that they published their channel participation
	PeerFilter(channel common.ChannelID, messagePredicate api.SubChannelSelectionCriteria) (filter.RoutingFilter, error)

	// Accept returns a dedicated read-only channel for messages sent by other nodes that match a certain predicate.
	// If passThrough is false, the messages are processed by the gossip layer beforehand.
	// If passThrough is true, the gossip layer doesn't intervene and the messages
	// can be used to send a reply back to the sender
	Accept(acceptor common.MessageAcceptor, passThrough bool) (<-chan *gproto.GossipMessage, <-chan protoext.ReceivedMessage)

	// JoinChan makes the Gossip instance join a channel
	JoinChan(joinMsg api.JoinChannelMessage, channelID common.ChannelID)

	// LeaveChan makes the Gossip instance leave a channel.
	// It still disseminates stateInfo message, but doesn't participate
	// in block pulling anymore, and can't return anymore a list of peers
	// in the channel.
	LeaveChan(channelID common.ChannelID)

	// SuspectPeers makes the gossip instance validate identities of suspected peers, and close
	// any connections to peers with identities that are found invalid
	SuspectPeers(s api.PeerSuspector)

	// IdentityInfo returns information known peer identities
	IdentityInfo() api.PeerIdentitySet

	// IsInMyOrg checks whether a network member is in this peer's org
	IsInMyOrg(member discovery.NetworkMember) bool

	// Stop stops the gossip component
	Stop()
}

// GossipServiceAdapter serves to provide basic functionality
// required from gossip service by delivery service
type GossipServiceAdapter interface {
	// PeersOfChannel returns slice with members of specified channel
	PeersOfChannel(gossipcommon.ChannelID) []discovery.NetworkMember

	// AddPayload adds payload to the local state sync buffer
	AddPayload(channelID string, payload *gproto.Payload) error

	// Gossip the message across the peers
	Gossip(msg *gproto.GossipMessage)
}

// DeliveryServiceFactory factory to create and initialize delivery service instance
type DeliveryServiceFactory interface {
	// Returns an instance of delivery client
	Service(g GossipServiceAdapter, endpoints []string, msc api.MessageCryptoService) (deliverservice.DeliverService, error)
}

type deliveryFactoryImpl struct {
	signer            identity.SignerSerializer
	credentialSupport *corecomm.CredentialSupport
}

// Returns an instance of delivery client
func (df *deliveryFactoryImpl) Service(g GossipServiceAdapter, endpoints []string, mcs api.MessageCryptoService) (deliverservice.DeliverService, error) {
	return deliverservice.NewDeliverService(&deliverservice.Config{
		CryptoSvc: mcs,
		Gossip:    g,
		Endpoints: endpoints,
		ConnFactory: (&deliverservice.CredSupportDialerFactory{
			CredentialSupport: df.credentialSupport,
			KeepaliveOptions:  deliverservice.KeepaliveOptions(),
		}).Dialer,
		ABCFactory: deliverservice.DefaultABCFactory,
		Signer:     df.signer,
	})
}

type privateHandler struct {
	support     Support
	coordinator gossipprivdata.Coordinator
	distributor gossipprivdata.PvtDataDistributor
	reconciler  gossipprivdata.PvtDataReconciler
}

func (p privateHandler) close() {
	p.coordinator.Close()
	p.reconciler.Stop()
}

type GossipService struct {
	gossipSvc
	privateHandlers map[string]privateHandler
	chains          map[string]state.GossipStateProvider
	leaderElection  map[string]election.LeaderElectionService
	deliveryService map[string]deliverservice.DeliverService
	deliveryFactory DeliveryServiceFactory
	lock            sync.RWMutex
	mcs             api.MessageCryptoService
	peerIdentity    []byte
	secAdv          api.SecurityAdvisor
	metrics         *gossipmetrics.GossipMetrics
	serviceConfig   *ServiceConfig
}

// This is an implementation of api.JoinChannelMessage.
type joinChannelMessage struct {
	seqNum              uint64
	members2AnchorPeers map[string][]api.AnchorPeer
}

func (jcm *joinChannelMessage) SequenceNumber() uint64 {
	return jcm.seqNum
}

// Members returns the organizations of the channel
func (jcm *joinChannelMessage) Members() []api.OrgIdentityType {
	members := make([]api.OrgIdentityType, 0, len(jcm.members2AnchorPeers))
	for org := range jcm.members2AnchorPeers {
		members = append(members, api.OrgIdentityType(org))
	}
	return members
}

// AnchorPeersOf returns the anchor peers of the given organization
func (jcm *joinChannelMessage) AnchorPeersOf(org api.OrgIdentityType) []api.AnchorPeer {
	return jcm.members2AnchorPeers[string(org)]
}

var logger = util.GetLogger(util.ServiceLogger, "")

// New creates the gossip service.
func New(
	peerIdentity identity.SignerSerializer,
	gossipMetrics *gossipmetrics.GossipMetrics,
	endpoint string,
	s *grpc.Server,
	certs *gossipcommon.TLSCertificates,
	mcs api.MessageCryptoService,
	secAdv api.SecurityAdvisor,
	secureDialOpts api.PeerSecureDialOpts,
	credSupport *corecomm.CredentialSupport,
	bootPeers ...string,
) (*GossipService, error) {
	serializedIdentity, err := peerIdentity.Serialize()
	if err != nil {
		return nil, err
	}

	serviceConfig := GlobalConfig()
	if serviceConfig.Endpoint != "" {
		endpoint = serviceConfig.Endpoint
	}

	gossipConfig, err := gossip.GlobalConfig(endpoint, certs, bootPeers...)
	if err != nil {
		return nil, err
	}

	logger.Infof("Initialize gossip with endpoint %s and bootstrap set %v", endpoint, bootPeers)

	gossipComponent := gossip.New(
		gossipConfig,
		s,
		secAdv,
		mcs,
		serializedIdentity, secureDialOpts,
		gossipMetrics,
	)

	return &GossipService{
		gossipSvc:       gossipComponent,
		mcs:             mcs,
		privateHandlers: make(map[string]privateHandler),
		chains:          make(map[string]state.GossipStateProvider),
		leaderElection:  make(map[string]election.LeaderElectionService),
		deliveryService: make(map[string]deliverservice.DeliverService),
		deliveryFactory: &deliveryFactoryImpl{signer: peerIdentity, credentialSupport: credSupport},
		peerIdentity:    serializedIdentity,
		secAdv:          secAdv,
		metrics:         gossipMetrics,
		serviceConfig:   serviceConfig,
	}, nil
}

// DistributePrivateData distribute private read write set inside the channel based on the collections policies
func (g *GossipService) DistributePrivateData(channelID string, txID string, privData *transientstore.TxPvtReadWriteSetWithConfigInfo, blkHt uint64) error {
	g.lock.RLock()
	handler, exists := g.privateHandlers[channelID]
	g.lock.RUnlock()
	if !exists {
		return errors.Errorf("No private data handler for %s", channelID)
	}

	if err := handler.distributor.Distribute(txID, privData, blkHt); err != nil {
		logger.Error("Failed to distributed private collection, txID", txID, "channel", channelID, "due to", err)
		return err
	}

	if err := handler.coordinator.StorePvtData(txID, privData, blkHt); err != nil {
		logger.Error("Failed to store private data into transient store, txID",
			txID, "channel", channelID, "due to", err)
		return err
	}
	return nil
}

// NewConfigEventer creates a ConfigProcessor which the channelconfig.BundleSource can ultimately route config updates to
func (g *GossipService) NewConfigEventer() ConfigProcessor {
	return newConfigEventer(g)
}

// Support aggregates functionality of several
// interfaces required by gossip service
type Support struct {
	Validator            txvalidator.Validator
	Committer            committer.Committer
	Store                gossipprivdata.TransientStore
	CollectionStore      privdata.CollectionStore
	IdDeserializeFactory gossipprivdata.IdentityDeserializerFactory
}

// DataStoreSupport aggregates interfaces capable
// of handling either incoming blocks or private data
type DataStoreSupport struct {
	committer.Committer
	gossipprivdata.TransientStore
}

// InitializeChannel allocates the state provider and should be invoked once per channel per execution
func (g *GossipService) InitializeChannel(channelID string, endpoints []string, support Support) {
	g.lock.Lock()
	defer g.lock.Unlock()
	// Initialize new state provider for given committer
	logger.Debug("Creating state provider for channelID", channelID)
	servicesAdapter := &state.ServicesMediator{GossipAdapter: g, MCSAdapter: g.mcs}

	// Embed transient store and committer APIs to fulfill
	// DataStore interface to capture ability of retrieving
	// private data
	storeSupport := &DataStoreSupport{
		TransientStore: support.Store,
		Committer:      support.Committer,
	}
	// Initialize private data fetcher
	dataRetriever := gossipprivdata.NewDataRetriever(storeSupport)
	collectionAccessFactory := gossipprivdata.NewCollectionAccessFactory(support.IdDeserializeFactory)
	fetcher := gossipprivdata.NewPuller(g.metrics.PrivdataMetrics, support.CollectionStore, g.gossipSvc, dataRetriever,
		collectionAccessFactory, channelID, g.serviceConfig.BtlPullMargin)

	coordinatorConfig := gossipprivdata.CoordinatorConfig{
		TransientBlockRetention: g.serviceConfig.TransientstoreMaxBlockRetention,
		PullRetryThreshold:      g.serviceConfig.PvtDataPullRetryThreshold,
	}
	coordinator := gossipprivdata.NewCoordinator(gossipprivdata.Support{
		ChainID:         channelID,
		CollectionStore: support.CollectionStore,
		Validator:       support.Validator,
		TransientStore:  support.Store,
		Committer:       support.Committer,
		Fetcher:         fetcher,
	}, g.createSelfSignedData(), g.metrics.PrivdataMetrics, coordinatorConfig)

	privdataConfig := gossipprivdata.GlobalConfig()
	var reconciler gossipprivdata.PvtDataReconciler

	if privdataConfig.ReconciliationEnabled {
		reconciler = gossipprivdata.NewReconciler(channelID, g.metrics.PrivdataMetrics,
			support.Committer, fetcher, privdataConfig)
	} else {
		reconciler = &gossipprivdata.NoOpReconciler{}
	}

	pushAckTimeout := g.serviceConfig.PvtDataPushAckTimeout
	g.privateHandlers[channelID] = privateHandler{
		support:     support,
		coordinator: coordinator,
		distributor: gossipprivdata.NewDistributor(channelID, g, collectionAccessFactory, g.metrics.PrivdataMetrics, pushAckTimeout),
		reconciler:  reconciler,
	}
	g.privateHandlers[channelID].reconciler.Start()

	blockingMode := !g.serviceConfig.NonBlockingCommitMode
	g.chains[channelID] = state.NewGossipStateProvider(channelID, servicesAdapter, coordinator,
		g.metrics.StateMetrics, blockingMode)
	if g.deliveryService[channelID] == nil {
		var err error
		g.deliveryService[channelID], err = g.deliveryFactory.Service(g, endpoints, g.mcs)
		if err != nil {
			logger.Warningf("Cannot create delivery client, due to %+v", errors.WithStack(err))
		}
	}

	// Delivery service might be nil only if it was not able to get connected
	// to the ordering service
	if g.deliveryService[channelID] != nil {
		// Parameters:
		//              - peer.gossip.useLeaderElection
		//              - peer.gossip.orgLeader
		//
		// are mutual exclusive, setting both to true is not defined, hence
		// peer will panic and terminate
		leaderElection := g.serviceConfig.UseLeaderElection
		isStaticOrgLeader := g.serviceConfig.OrgLeader

		if leaderElection && isStaticOrgLeader {
			logger.Panic("Setting both orgLeader and useLeaderElection to true isn't supported, aborting execution")
		}

		if leaderElection {
			logger.Debug("Delivery uses dynamic leader election mechanism, channel", channelID)
			g.leaderElection[channelID] = g.newLeaderElectionComponent(channelID, g.onStatusChangeFactory(channelID,
				support.Committer), g.metrics.ElectionMetrics)
		} else if isStaticOrgLeader {
			logger.Debug("This peer is configured to connect to ordering service for blocks delivery, channel", channelID)
			g.deliveryService[channelID].StartDeliverForChannel(channelID, support.Committer, func() {})
		} else {
			logger.Debug("This peer is not configured to connect to ordering service for blocks delivery, channel", channelID)
		}
	} else {
		logger.Warning("Delivery client is down won't be able to pull blocks for chain", channelID)
	}
}

func (g *GossipService) createSelfSignedData() protoutil.SignedData {
	msg := make([]byte, 32)
	sig, err := g.mcs.Sign(msg)
	if err != nil {
		logger.Panicf("Failed creating self signed data because message signing failed: %v", err)
	}
	return protoutil.SignedData{
		Data:      msg,
		Signature: sig,
		Identity:  g.peerIdentity,
	}
}

// updateAnchors constructs a joinChannelMessage and sends it to the gossipSvc
func (g *GossipService) updateAnchors(config Config) {
	myOrg := string(g.secAdv.OrgByPeerIdentity(api.PeerIdentityType(g.peerIdentity)))
	if !g.amIinChannel(myOrg, config) {
		logger.Error("Tried joining channel", config.ChainID(), "but our org(", myOrg, "), isn't "+
			"among the orgs of the channel:", orgListFromConfig(config), ", aborting.")
		return
	}
	jcm := &joinChannelMessage{seqNum: config.Sequence(), members2AnchorPeers: map[string][]api.AnchorPeer{}}
	for _, appOrg := range config.Organizations() {
		logger.Debug(appOrg.MSPID(), "anchor peers:", appOrg.AnchorPeers())
		jcm.members2AnchorPeers[appOrg.MSPID()] = []api.AnchorPeer{}
		for _, ap := range appOrg.AnchorPeers() {
			anchorPeer := api.AnchorPeer{
				Host: ap.Host,
				Port: int(ap.Port),
			}
			jcm.members2AnchorPeers[appOrg.MSPID()] = append(jcm.members2AnchorPeers[appOrg.MSPID()], anchorPeer)
		}
	}

	// Initialize new state provider for given committer
	logger.Debug("Creating state provider for channelID", config.ChainID())
	g.JoinChan(jcm, gossipcommon.ChannelID(config.ChainID()))
}

func (g *GossipService) updateEndpoints(channelID string, endpoints []string) {
	if ds, ok := g.deliveryService[channelID]; ok {
		logger.Debugf("Updating endpoints for channelID %s", channelID)
		if err := ds.UpdateEndpoints(channelID, endpoints); err != nil {
			// The only reason to fail is because of absence of block provider
			// for given channel id, hence printing a warning will be enough
			logger.Warningf("Failed to update ordering service endpoints, due to %s", err)
		}
	}
}

// AddPayload appends message payload to for given chain
func (g *GossipService) AddPayload(channelID string, payload *gproto.Payload) error {
	g.lock.RLock()
	defer g.lock.RUnlock()
	return g.chains[channelID].AddPayload(payload)
}

// Stop stops the gossip component
func (g *GossipService) Stop() {
	g.lock.Lock()
	defer g.lock.Unlock()

	for chainID := range g.chains {
		logger.Info("Stopping chain", chainID)
		if le, exists := g.leaderElection[chainID]; exists {
			logger.Infof("Stopping leader election for %s", chainID)
			le.Stop()
		}
		g.chains[chainID].Stop()
		g.privateHandlers[chainID].close()

		if g.deliveryService[chainID] != nil {
			g.deliveryService[chainID].Stop()
		}
	}
	g.gossipSvc.Stop()
}

func (g *GossipService) newLeaderElectionComponent(channelID string, callback func(bool),
	electionMetrics *gossipmetrics.ElectionMetrics) election.LeaderElectionService {
	PKIid := g.mcs.GetPKIidOfCert(g.peerIdentity)
	adapter := election.NewAdapter(g, PKIid, gossipcommon.ChannelID(channelID), electionMetrics)
	config := election.ElectionConfig{
		StartupGracePeriod:       g.serviceConfig.ElectionStartupGracePeriod,
		MembershipSampleInterval: g.serviceConfig.ElectionMembershipSampleInterval,
		LeaderAliveThreshold:     g.serviceConfig.ElectionLeaderAliveThreshold,
		LeaderElectionDuration:   g.serviceConfig.ElectionLeaderElectionDuration,
	}
	return election.NewLeaderElectionService(adapter, string(PKIid), callback, config)
}

func (g *GossipService) amIinChannel(myOrg string, config Config) bool {
	for _, orgName := range orgListFromConfig(config) {
		if orgName == myOrg {
			return true
		}
	}
	return false
}

func (g *GossipService) onStatusChangeFactory(channelID string, committer blocksprovider.LedgerInfo) func(bool) {
	return func(isLeader bool) {
		if isLeader {
			yield := func() {
				g.lock.RLock()
				le := g.leaderElection[channelID]
				g.lock.RUnlock()
				le.Yield()
			}
			logger.Info("Elected as a leader, starting delivery service for channel", channelID)
			if err := g.deliveryService[channelID].StartDeliverForChannel(channelID, committer, yield); err != nil {
				logger.Errorf("Delivery service is not able to start blocks delivery for chain, due to %+v", err)
			}
		} else {
			logger.Info("Renounced leadership, stopping delivery service for channel", channelID)
			if err := g.deliveryService[channelID].StopDeliverForChannel(channelID); err != nil {
				logger.Errorf("Delivery service is not able to stop blocks delivery for chain, due to %+v", err)
			}
		}
	}
}

func orgListFromConfig(config Config) []string {
	var orgList []string
	for _, appOrg := range config.Organizations() {
		orgList = append(orgList, appOrg.MSPID())
	}
	return orgList
}
