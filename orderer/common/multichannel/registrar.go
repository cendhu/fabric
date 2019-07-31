/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

// Package multichannel tracks the channel resources for the orderer.  It initially
// loads the set of existing channels, and provides an interface for users of these
// channels to retrieve them, or create new ones.
package multichannel

import (
	"fmt"
	"sync"

	cb "github.com/hyperledger/fabric-protos-go/common"
	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.com/hyperledger/fabric/bccsp"
	"github.com/hyperledger/fabric/common/channelconfig"
	"github.com/hyperledger/fabric/common/configtx"
	"github.com/hyperledger/fabric/common/flogging"
	"github.com/hyperledger/fabric/common/ledger/blockledger"
	"github.com/hyperledger/fabric/common/metrics"
	"github.com/hyperledger/fabric/internal/pkg/identity"
	"github.com/hyperledger/fabric/orderer/common/blockcutter"
	"github.com/hyperledger/fabric/orderer/common/localconfig"
	"github.com/hyperledger/fabric/orderer/common/msgprocessor"
	"github.com/hyperledger/fabric/orderer/consensus"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/pkg/errors"
)

const (
	msgVersion = int32(0)
	epoch      = 0
)

var logger = flogging.MustGetLogger("orderer.commmon.multichannel")

// checkResources makes sure that the channel config is compatible with this binary and logs sanity checks
func checkResources(res channelconfig.Resources) error {
	channelconfig.LogSanityChecks(res)
	oc, ok := res.OrdererConfig()
	if !ok {
		return errors.New("config does not contain orderer config")
	}
	if err := oc.Capabilities().Supported(); err != nil {
		return errors.Wrapf(err, "config requires unsupported orderer capabilities: %s", err)
	}
	if err := res.ChannelConfig().Capabilities().Supported(); err != nil {
		return errors.Wrapf(err, "config requires unsupported channel capabilities: %s", err)
	}
	return nil
}

// checkResourcesOrPanic invokes checkResources and panics if an error is returned
func checkResourcesOrPanic(res channelconfig.Resources) {
	if err := checkResources(res); err != nil {
		logger.Panicf("[channel %s] %s", res.ConfigtxValidator().ChannelID(), err)
	}
}

type mutableResources interface {
	channelconfig.Resources
	Update(*channelconfig.Bundle)
}

type configResources struct {
	mutableResources
	bccsp bccsp.BCCSP
}

func (cr *configResources) CreateBundle(channelID string, config *cb.Config) (*channelconfig.Bundle, error) {
	return channelconfig.NewBundle(channelID, config, cr.bccsp)
}

func (cr *configResources) Update(bndl *channelconfig.Bundle) {
	checkResourcesOrPanic(bndl)
	cr.mutableResources.Update(bndl)
}

func (cr *configResources) SharedConfig() channelconfig.Orderer {
	oc, ok := cr.OrdererConfig()
	if !ok {
		logger.Panicf("[channel %s] has no orderer configuration", cr.ConfigtxValidator().ChannelID())
	}
	return oc
}

type ledgerResources struct {
	*configResources
	blockledger.ReadWriter
}

// Registrar serves as a point of access and control for the individual channel resources.
type Registrar struct {
	config localconfig.TopLevel
	lock   sync.RWMutex
	chains map[string]*ChainSupport

	consenters         map[string]consensus.Consenter
	ledgerFactory      blockledger.Factory
	signer             identity.SignerSerializer
	blockcutterMetrics *blockcutter.Metrics
	systemChannelID    string
	systemChannel      *ChainSupport
	templator          msgprocessor.ChannelConfigTemplator
	callbacks          []channelconfig.BundleActor
	bccsp              bccsp.BCCSP
}

// ConfigBlock retrieves the last configuration block from the given ledger.
// Panics on failure.
func ConfigBlock(reader blockledger.Reader) *cb.Block {
	lastBlock := blockledger.GetBlock(reader, reader.Height()-1)
	index, err := protoutil.GetLastConfigIndexFromBlock(lastBlock)
	if err != nil {
		logger.Panicf("Chain did not have appropriately encoded last config in its latest block: %s", err)
	}
	configBlock := blockledger.GetBlock(reader, index)
	if configBlock == nil {
		logger.Panicf("Config block does not exist")
	}

	return configBlock
}

func configTx(reader blockledger.Reader) *cb.Envelope {
	return protoutil.ExtractEnvelopeOrPanic(ConfigBlock(reader), 0)
}

// NewRegistrar produces an instance of a *Registrar.
func NewRegistrar(
	config localconfig.TopLevel,
	ledgerFactory blockledger.Factory,
	signer identity.SignerSerializer,
	metricsProvider metrics.Provider,
	bccsp bccsp.BCCSP,
	callbacks ...channelconfig.BundleActor,
) *Registrar {
	r := &Registrar{
		config:             config,
		chains:             make(map[string]*ChainSupport),
		ledgerFactory:      ledgerFactory,
		signer:             signer,
		blockcutterMetrics: blockcutter.NewMetrics(metricsProvider),
		callbacks:          callbacks,
		bccsp:              bccsp,
	}

	return r
}

func (r *Registrar) Initialize(consenters map[string]consensus.Consenter) {
	r.consenters = consenters
	existingChannels := r.ledgerFactory.ChannelIDs()

	for _, channelID := range existingChannels {
		rl, err := r.ledgerFactory.GetOrCreate(channelID)
		if err != nil {
			logger.Panicf("Ledger factory reported channelID %s but could not retrieve it: %s", channelID, err)
		}
		configTx := configTx(rl)
		if configTx == nil {
			logger.Panic("Programming error, configTx should never be nil here")
		}
		ledgerResources := r.newLedgerResources(configTx)
		channelID := ledgerResources.ConfigtxValidator().ChannelID()

		if _, ok := ledgerResources.ConsortiumsConfig(); ok {
			if r.systemChannelID != "" {
				logger.Panicf("There appear to be two system channels %s and %s", r.systemChannelID, channelID)
			}

			chain := newChainSupport(
				r,
				ledgerResources,
				r.consenters,
				r.signer,
				r.blockcutterMetrics,
				r.bccsp,
			)
			r.templator = msgprocessor.NewDefaultTemplator(chain, r.bccsp)
			chain.Processor = msgprocessor.NewSystemChannel(
				chain,
				r.templator,
				msgprocessor.CreateSystemChannelFilters(r.config, r, chain, chain.MetadataValidator),
				r.bccsp,
			)

			// Retrieve genesis block to log its hash. See FAB-5450 for the purpose
			iter, pos := rl.Iterator(&ab.SeekPosition{Type: &ab.SeekPosition_Oldest{Oldest: &ab.SeekOldest{}}})
			defer iter.Close()
			if pos != uint64(0) {
				logger.Panicf("Error iterating over system channel: '%s', expected position 0, got %d", channelID, pos)
			}
			genesisBlock, status := iter.Next()
			if status != cb.Status_SUCCESS {
				logger.Panicf("Error reading genesis block of system channel '%s'", channelID)
			}
			logger.Infof("Starting system channel '%s' with genesis block hash %x and orderer type %s",
				channelID, protoutil.BlockHeaderHash(genesisBlock.Header), chain.SharedConfig().ConsensusType())

			r.chains[channelID] = chain
			r.systemChannelID = channelID
			r.systemChannel = chain
			// We delay starting this channel, as it might try to copy and replace the channels map via newChannel before the map is fully built
			defer chain.start()
		} else {
			logger.Debugf("Starting channel: %s", channelID)
			chain := newChainSupport(
				r,
				ledgerResources,
				r.consenters,
				r.signer,
				r.blockcutterMetrics,
				r.bccsp,
			)
			r.chains[channelID] = chain
			chain.start()
		}

	}

	if r.systemChannelID == "" {
		logger.Panicf("No system chain found.  If bootstrapping, does your system channel contain a consortiums group definition?")
	}
}

// SystemChannelID returns the ChannelID for the system channel.
func (r *Registrar) SystemChannelID() string {
	return r.systemChannelID
}

// BroadcastChannelSupport returns the message channel header, whether the message is a config update
// and the channel resources for a message or an error if the message is not a message which can
// be processed directly (like CONFIG and ORDERER_TRANSACTION messages)
func (r *Registrar) BroadcastChannelSupport(msg *cb.Envelope) (*cb.ChannelHeader, bool, *ChainSupport, error) {
	chdr, err := protoutil.ChannelHeader(msg)
	if err != nil {
		return nil, false, nil, fmt.Errorf("could not determine channel ID: %s", err)
	}

	cs := r.GetChain(chdr.ChannelId)
	// New channel creation
	if cs == nil {
		cs = r.systemChannel
	}

	isConfig := false
	switch cs.ClassifyMsg(chdr) {
	case msgprocessor.ConfigUpdateMsg:
		isConfig = true
	case msgprocessor.ConfigMsg:
		return chdr, false, nil, errors.New("message is of type that cannot be processed directly")
	default:
	}

	return chdr, isConfig, cs, nil
}

// GetChain retrieves the chain support for a chain if it exists.
func (r *Registrar) GetChain(chainID string) *ChainSupport {
	r.lock.RLock()
	defer r.lock.RUnlock()

	return r.chains[chainID]
}

func (r *Registrar) newLedgerResources(configTx *cb.Envelope) *ledgerResources {
	payload, err := protoutil.UnmarshalPayload(configTx.Payload)
	if err != nil {
		logger.Panicf("Error umarshaling envelope to payload: %s", err)
	}

	if payload.Header == nil {
		logger.Panicf("Missing channel header: %s", err)
	}

	chdr, err := protoutil.UnmarshalChannelHeader(payload.Header.ChannelHeader)
	if err != nil {
		logger.Panicf("Error unmarshaling channel header: %s", err)
	}

	configEnvelope, err := configtx.UnmarshalConfigEnvelope(payload.Data)
	if err != nil {
		logger.Panicf("Error umarshaling config envelope from payload data: %s", err)
	}

	bundle, err := channelconfig.NewBundle(chdr.ChannelId, configEnvelope.Config, r.bccsp)
	if err != nil {
		logger.Panicf("Error creating channelconfig bundle: %s", err)
	}

	checkResourcesOrPanic(bundle)

	ledger, err := r.ledgerFactory.GetOrCreate(chdr.ChannelId)
	if err != nil {
		logger.Panicf("Error getting ledger for %s", chdr.ChannelId)
	}

	return &ledgerResources{
		configResources: &configResources{
			mutableResources: channelconfig.NewBundleSource(bundle, r.callbacks...),
			bccsp:            r.bccsp,
		},
		ReadWriter: ledger,
	}
}

// CreateChain makes the Registrar create a chain with the given name.
func (r *Registrar) CreateChain(chainName string) {
	lf, err := r.ledgerFactory.GetOrCreate(chainName)
	if err != nil {
		logger.Panicf("Failed obtaining ledger factory for %s: %v", chainName, err)
	}
	chain := r.GetChain(chainName)
	if chain != nil {
		logger.Infof("A chain of type %T for channel %s already exists. "+
			"Halting it.", chain.Chain, chainName)
		chain.Halt()
	}
	r.newChain(configTx(lf))
}

func (r *Registrar) newChain(configtx *cb.Envelope) {
	r.lock.Lock()
	defer r.lock.Unlock()

	ledgerResources := r.newLedgerResources(configtx)
	// If we have no blocks, we need to create the genesis block ourselves.
	if ledgerResources.Height() == 0 {
		ledgerResources.Append(blockledger.CreateNextBlock(ledgerResources, []*cb.Envelope{configtx}))
	}

	// Copy the map to allow concurrent reads from broadcast/deliver while the new chainSupport is
	newChains := make(map[string]*ChainSupport)
	for key, value := range r.chains {
		newChains[key] = value
	}

	cs := newChainSupport(r, ledgerResources, r.consenters, r.signer, r.blockcutterMetrics, r.bccsp)
	chainID := ledgerResources.ConfigtxValidator().ChannelID()

	logger.Infof("Created and starting new channel %s", chainID)

	newChains[string(chainID)] = cs
	cs.start()

	r.chains = newChains
}

// ChannelsCount returns the count of the current total number of channels.
func (r *Registrar) ChannelsCount() int {
	r.lock.RLock()
	defer r.lock.RUnlock()

	return len(r.chains)
}

// NewChannelConfig produces a new template channel configuration based on the system channel's current config.
func (r *Registrar) NewChannelConfig(envConfigUpdate *cb.Envelope) (channelconfig.Resources, error) {
	return r.templator.NewChannelConfig(envConfigUpdate)
}

// CreateBundle calls channelconfig.NewBundle
func (r *Registrar) CreateBundle(channelID string, config *cb.Config) (channelconfig.Resources, error) {
	return channelconfig.NewBundle(channelID, config, r.bccsp)
}
