/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package chaincode

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/hyperledger/fabric/core/chaincode/accesscontrol"
	"github.com/hyperledger/fabric/core/common/ccprovider"
	"github.com/hyperledger/fabric/core/container"
	"github.com/hyperledger/fabric/core/container/ccintf"
	pb "github.com/hyperledger/fabric/protos/peer"
	"github.com/pkg/errors"
)

// CertGenerator generates client certificates for chaincode.
type CertGenerator interface {
	// Generate returns a certificate and private key and associates
	// the hash of the certificates with the given chaincode name
	Generate(ccName string) (*accesscontrol.CertAndPrivKeyPair, error)
}

// ContainerRuntime is responsible for managing containerized chaincode.
type ContainerRuntime struct {
	CertGenerator  CertGenerator
	VMSynchronizer *container.VMController
	CACert         []byte
	CommonEnv      []string
	PeerAddress    string
}

// Start launches chaincode in a runtime environment.
func (c *ContainerRuntime) Start(ccci *ccprovider.ChaincodeContainerInfo, codePackage []byte) error {
	vm, ok := c.VMSynchronizer.GetLockingVM(ccci.ContainerType)
	if !ok {
		return errors.Errorf("unknown container type: %s", ccci.ContainerType)
	}

	packageID := ccci.PackageID.String()

	lc, err := c.LaunchConfig(packageID, ccci.Type)
	if err != nil {
		return err
	}

	if err := vm.Build(
		ccintf.New(ccci.PackageID),
		ccci.Type,
		ccci.Path,
		ccci.Name,
		ccci.Version,
		codePackage,
	); err != nil {
		return errors.WithMessage(err, "error building image")
	}

	chaincodeLogger.Debugf("start container: %s", packageID)
	chaincodeLogger.Debugf("start container with args: %s", strings.Join(lc.Args, " "))
	chaincodeLogger.Debugf("start container with env:\n\t%s", strings.Join(lc.Envs, "\n\t"))

	if err := vm.Start(
		ccintf.New(ccci.PackageID),
		lc.Args,
		lc.Envs,
		lc.Files,
	); err != nil {
		return errors.WithMessage(err, "error starting container")
	}

	return nil
}

// Stop terminates chaincode and its container runtime environment.
func (c *ContainerRuntime) Stop(ccci *ccprovider.ChaincodeContainerInfo) error {
	vm, ok := c.VMSynchronizer.GetLockingVM(ccci.ContainerType)
	if !ok {
		return errors.Errorf("unknown container type: %s", ccci.ContainerType)
	}

	if err := vm.Stop(ccintf.New(ccci.PackageID)); err != nil {
		return errors.WithMessage(err, "error stopping container")
	}

	return nil
}

// Wait waits for the container runtime to terminate.
func (c *ContainerRuntime) Wait(ccci *ccprovider.ChaincodeContainerInfo) (int, error) {
	vm, ok := c.VMSynchronizer.GetLockingVM(ccci.ContainerType)
	if !ok {
		return 0, errors.Errorf("unknown container type: %s", ccci.ContainerType)
	}

	return vm.Wait(ccintf.New(ccci.PackageID))
}

const (
	// Mutual TLS auth client key and cert paths in the chaincode container
	TLSClientKeyPath      string = "/etc/hyperledger/fabric/client.key"
	TLSClientCertPath     string = "/etc/hyperledger/fabric/client.crt"
	TLSClientRootCertPath string = "/etc/hyperledger/fabric/peer.crt"
)

func (c *ContainerRuntime) getTLSFiles(keyPair *accesscontrol.CertAndPrivKeyPair) map[string][]byte {
	if keyPair == nil {
		return nil
	}

	return map[string][]byte{
		TLSClientKeyPath:      []byte(keyPair.Key),
		TLSClientCertPath:     []byte(keyPair.Cert),
		TLSClientRootCertPath: c.CACert,
	}
}

// LaunchConfig holds chaincode launch arguments, environment variables, and files.
type LaunchConfig struct {
	Args  []string
	Envs  []string
	Files map[string][]byte
}

// LaunchConfig creates the LaunchConfig for chaincode running in a container.
func (c *ContainerRuntime) LaunchConfig(packageID string, ccType string) (*LaunchConfig, error) {
	var lc LaunchConfig

	// common environment variables
	// FIXME: we are using the env variable CHAINCODE_ID to store
	// the package ID; in the legacy lifecycle they used to be the
	// same but now they are not, so we should use a different env
	// variable. However chaincodes built by older versions of the
	// peer still adopt this broken convention. (FAB-14630)
	lc.Envs = append(c.CommonEnv, "CORE_CHAINCODE_ID_NAME="+packageID)

	// language specific arguments
	switch ccType {
	case pb.ChaincodeSpec_GOLANG.String(), pb.ChaincodeSpec_CAR.String():
		lc.Args = []string{"chaincode", fmt.Sprintf("-peer.address=%s", c.PeerAddress)}
	case pb.ChaincodeSpec_JAVA.String():
		lc.Args = []string{"/root/chaincode-java/start", "--peerAddress", c.PeerAddress}
	case pb.ChaincodeSpec_NODE.String():
		lc.Args = []string{"/bin/sh", "-c", fmt.Sprintf("cd /usr/local/src; npm start -- --peer.address %s", c.PeerAddress)}
	default:
		return nil, errors.Errorf("unknown chaincodeType: %s", ccType)
	}

	// Pass TLS options to chaincode
	if c.CertGenerator != nil {
		certKeyPair, err := c.CertGenerator.Generate(packageID)
		if err != nil {
			return nil, errors.WithMessagef(err, "failed to generate TLS certificates for %s", packageID)
		}
		lc.Files = c.getTLSFiles(certKeyPair)
		if lc.Files == nil {
			return nil, errors.Errorf("failed to acquire TLS certificates for %s", packageID)
		}

		lc.Envs = append(lc.Envs, "CORE_PEER_TLS_ENABLED=true")
		lc.Envs = append(lc.Envs, fmt.Sprintf("CORE_TLS_CLIENT_KEY_PATH=%s", TLSClientKeyPath))
		lc.Envs = append(lc.Envs, fmt.Sprintf("CORE_TLS_CLIENT_CERT_PATH=%s", TLSClientCertPath))
		lc.Envs = append(lc.Envs, fmt.Sprintf("CORE_PEER_TLS_ROOTCERT_FILE=%s", TLSClientRootCertPath))
	} else {
		lc.Envs = append(lc.Envs, "CORE_PEER_TLS_ENABLED=false")
	}

	chaincodeLogger.Debugf("launchConfig: %s", lc.String())

	return &lc, nil
}

func (lc *LaunchConfig) String() string {
	buf := &bytes.Buffer{}
	if len(lc.Args) > 0 {
		fmt.Fprintf(buf, "executable:%q,", lc.Args[0])
	}

	fileNames := []string{}
	for k := range lc.Files {
		fileNames = append(fileNames, k)
	}
	sort.Strings(fileNames)

	fmt.Fprintf(buf, "Args:[%s],", strings.Join(lc.Args, ","))
	fmt.Fprintf(buf, "Envs:[%s],", strings.Join(lc.Envs, ","))
	fmt.Fprintf(buf, "Files:%v", fileNames)
	return buf.String()
}
