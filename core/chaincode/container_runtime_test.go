/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package chaincode_test

import (
	"testing"

	"github.com/hyperledger/fabric/core/chaincode"
	"github.com/hyperledger/fabric/core/chaincode/accesscontrol"
	"github.com/hyperledger/fabric/core/chaincode/mock"
	persistence "github.com/hyperledger/fabric/core/chaincode/persistence/intf"
	"github.com/hyperledger/fabric/core/common/ccprovider"
	"github.com/hyperledger/fabric/core/container"
	"github.com/hyperledger/fabric/core/container/ccintf"
	pb "github.com/hyperledger/fabric/protos/peer"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
)

func TestLaunchConfigString(t *testing.T) {
	tests := []struct {
		lc  *chaincode.LaunchConfig
		str string
	}{
		{&chaincode.LaunchConfig{}, `Args:[],Envs:[],Files:[]`},
		{&chaincode.LaunchConfig{Args: []string{"command", "arg1", "arg2"}}, `executable:"command",Args:[command,arg1,arg2],Envs:[],Files:[]`},
		{&chaincode.LaunchConfig{Envs: []string{"ENV1=VALUE1", "ENV2=VALUE2"}}, `Args:[],Envs:[ENV1=VALUE1,ENV2=VALUE2],Files:[]`},
		{&chaincode.LaunchConfig{Files: map[string][]byte{"key1": []byte("value1"), "key2": []byte("value2")}}, `Args:[],Envs:[],Files:[key1 key2]`},
		{
			&chaincode.LaunchConfig{
				Args:  []string{"command", "arg1", "arg2"},
				Envs:  []string{"ENV1=VALUE1", "ENV2=VALUE2"},
				Files: map[string][]byte{"key1": []byte("value1"), "key2": []byte("value2")},
			},
			`executable:"command",Args:[command,arg1,arg2],Envs:[ENV1=VALUE1,ENV2=VALUE2],Files:[key1 key2]`,
		},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.str, tc.lc.String())
	}
}

func TestContainerRuntimeLaunchConfigArgs(t *testing.T) {
	tests := []struct {
		name         string
		ccType       pb.ChaincodeSpec_Type
		expectedArgs []string
		expectedErr  string
	}{
		{"car-chaincode", pb.ChaincodeSpec_CAR, []string{"chaincode", "-peer.address=peer-address"}, ""},
		{"golang-chaincode", pb.ChaincodeSpec_GOLANG, []string{"chaincode", "-peer.address=peer-address"}, ""},
		{"java-chaincode", pb.ChaincodeSpec_JAVA, []string{"/root/chaincode-java/start", "--peerAddress", "peer-address"}, ""},
		{"node-chaincode", pb.ChaincodeSpec_NODE, []string{"/bin/sh", "-c", "cd /usr/local/src; npm start -- --peer.address peer-address"}, ""},
		{"unknown-chaincode", pb.ChaincodeSpec_Type(999), []string{}, "unknown chaincodeType: 999"},
	}
	for _, tc := range tests {
		cr := &chaincode.ContainerRuntime{
			CommonEnv:   []string{},
			PeerAddress: "peer-address",
		}

		lc, err := cr.LaunchConfig(tc.name, tc.ccType.String())
		if tc.expectedErr != "" {
			assert.EqualError(t, err, tc.expectedErr)
			continue
		}
		assert.NoError(t, err)
		assert.Equal(t, tc.expectedArgs, lc.Args)
	}
}

func TestContainerRuntimeLaunchConfigEnv(t *testing.T) {
	commonEnv := []string{
		"COMMON_1=VALUE1",
		"COMMON_2=VALUE2",
	}
	disabledTLSEnv := []string{
		"CORE_PEER_TLS_ENABLED=false",
	}
	enabledTLSEnv := []string{
		"CORE_PEER_TLS_ENABLED=true",
		"CORE_TLS_CLIENT_KEY_PATH=/etc/hyperledger/fabric/client.key",
		"CORE_TLS_CLIENT_CERT_PATH=/etc/hyperledger/fabric/client.crt",
		"CORE_PEER_TLS_ROOTCERT_FILE=/etc/hyperledger/fabric/peer.crt",
	}

	certGenerator := &mock.CertGenerator{}
	certGenerator.GenerateReturns(&accesscontrol.CertAndPrivKeyPair{Cert: "certificate", Key: "key"}, nil)

	tests := []struct {
		name          string
		certGenerator *mock.CertGenerator
		expectedEnv   []string
	}{
		{"tls-disabled", nil, append([]string{"CORE_CHAINCODE_ID_NAME=tls-disabled"}, disabledTLSEnv...)},
		{"tls-enabled", certGenerator, append([]string{"CORE_CHAINCODE_ID_NAME=tls-enabled"}, enabledTLSEnv...)},
	}

	for _, tc := range tests {
		cr := &chaincode.ContainerRuntime{
			CommonEnv:   commonEnv,
			PeerAddress: "peer-address",
		}
		if tc.certGenerator != nil {
			cr.CertGenerator = tc.certGenerator
		}

		lc, err := cr.LaunchConfig(tc.name, pb.ChaincodeSpec_GOLANG.String())
		assert.NoError(t, err)
		assert.Equal(t, append(commonEnv, tc.expectedEnv...), lc.Envs)
		if tc.certGenerator != nil {
			assert.Equal(t, 1, certGenerator.GenerateCallCount())
			assert.Equal(t, tc.name, certGenerator.GenerateArgsForCall(0))
		}
	}
}

func TestContainerRuntimeLaunchConfigFiles(t *testing.T) {
	keyPair := &accesscontrol.CertAndPrivKeyPair{Cert: "certificate", Key: "key"}
	certGenerator := &mock.CertGenerator{}
	certGenerator.GenerateReturns(keyPair, nil)
	cr := &chaincode.ContainerRuntime{
		CACert:        []byte("peer-ca-cert"),
		CertGenerator: certGenerator,
	}

	lc, err := cr.LaunchConfig("chaincode-name", pb.ChaincodeSpec_GOLANG.String())
	assert.NoError(t, err)
	assert.Equal(t, map[string][]byte{
		"/etc/hyperledger/fabric/client.crt": []byte("certificate"),
		"/etc/hyperledger/fabric/client.key": []byte("key"),
		"/etc/hyperledger/fabric/peer.crt":   []byte("peer-ca-cert"),
	}, lc.Files)
}

func TestContainerRuntimeLaunchConfigGenerateFail(t *testing.T) {
	tests := []struct {
		keyPair     *accesscontrol.CertAndPrivKeyPair
		generateErr error
		errValue    string
	}{
		{nil, nil, "failed to acquire TLS certificates for chaincode-id"},
		{nil, errors.New("no-cert-for-you"), "failed to generate TLS certificates for chaincode-id: no-cert-for-you"},
	}

	for _, tc := range tests {
		certGenerator := &mock.CertGenerator{}
		certGenerator.GenerateReturns(tc.keyPair, tc.generateErr)
		cr := &chaincode.ContainerRuntime{CertGenerator: certGenerator}

		_, err := cr.LaunchConfig("chaincode-id", pb.ChaincodeSpec_GOLANG.String())
		assert.EqualError(t, err, tc.errValue)
	}
}

func TestContainerRuntimeStart(t *testing.T) {
	fakeVM := &mock.ContainerVM{}

	cr := &chaincode.ContainerRuntime{
		VMSynchronizer: container.NewVMController(map[string]container.VM{
			"DOCKER": fakeVM,
		}),
		PeerAddress: "peer.example.com",
	}

	ccci := &ccprovider.ChaincodeContainerInfo{
		Type:          "GOLANG",
		Path:          "chaincode-path",
		Name:          "chaincode-name",
		Version:       "chaincode-version",
		ContainerType: "DOCKER",
		PackageID:     "chaincode-name:chaincode-version",
	}

	err := cr.Start(ccci, []byte("code-package"))
	assert.NoError(t, err)

	assert.Equal(t, 1, fakeVM.BuildCallCount())
	ccid, ccType, path, name, version, codePackage := fakeVM.BuildArgsForCall(0)
	assert.Equal(t, ccintf.New("chaincode-name:chaincode-version"), ccid)
	assert.Equal(t, "GOLANG", ccType)
	assert.Equal(t, "chaincode-path", path)
	assert.Equal(t, "chaincode-name", name)
	assert.Equal(t, "chaincode-version", version)
	assert.Equal(t, []byte("code-package"), codePackage)

	assert.Equal(t, 1, fakeVM.StartCallCount())
	ccid, args, env, filesToUpload := fakeVM.StartArgsForCall(0)
	assert.Equal(t, ccintf.CCID("chaincode-name:chaincode-version"), ccid)
	assert.Equal(t, []string{"chaincode", "-peer.address=peer.example.com"}, args)
	assert.Equal(t, []string{"CORE_CHAINCODE_ID_NAME=chaincode-name:chaincode-version", "CORE_PEER_TLS_ENABLED=false"}, env)
	assert.Nil(t, filesToUpload)
}

func TestContainerRuntimeStartErrors(t *testing.T) {
	tests := []struct {
		chaincodeType string
		buildErr      error
		startErr      error
		containerType string
		errValue      string
	}{
		{"bad-type", nil, nil, "DOCKER", "unknown chaincodeType: bad-type"},
		{pb.ChaincodeSpec_GOLANG.String(), nil, nil, "BADVM", "unknown container type: BADVM"},
		{pb.ChaincodeSpec_GOLANG.String(), nil, errors.New("process-failed"), "DOCKER", "error starting container: process-failed"},
		{pb.ChaincodeSpec_GOLANG.String(), errors.New("build-failed"), nil, "DOCKER", "error building image: build-failed"},
		{pb.ChaincodeSpec_GOLANG.String(), errors.New("build-failed"), nil, "DOCKER", "error building image: build-failed"},
	}

	for _, tc := range tests {
		fakeVM := &mock.ContainerVM{}
		fakeVM.BuildReturns(tc.buildErr)
		fakeVM.StartReturns(tc.startErr)

		cr := &chaincode.ContainerRuntime{
			VMSynchronizer: container.NewVMController(map[string]container.VM{
				"DOCKER": fakeVM,
			}),
			PeerAddress: "peer.example.com",
		}

		ccci := &ccprovider.ChaincodeContainerInfo{
			Type:          tc.chaincodeType,
			Name:          "chaincode-id-name",
			Version:       "chaincode-version",
			ContainerType: tc.containerType,
		}

		err := cr.Start(ccci, nil)
		assert.EqualError(t, err, tc.errValue)
	}
}

func TestContainerRuntimeStop(t *testing.T) {
	fakeVM := &mock.ContainerVM{}

	cr := &chaincode.ContainerRuntime{
		VMSynchronizer: container.NewVMController(map[string]container.VM{
			"DOCKER": fakeVM,
		}),
		PeerAddress: "peer.example.com",
	}

	ccci := &ccprovider.ChaincodeContainerInfo{
		Type:          pb.ChaincodeSpec_GOLANG.String(),
		ContainerType: "DOCKER",
		PackageID:     "chaincode-id-name:chaincode-version",
	}

	err := cr.Stop(ccci)
	assert.NoError(t, err)

	assert.Equal(t, 1, fakeVM.StopCallCount())
	ccid := fakeVM.StopArgsForCall(0)
	assert.Equal(t, ccintf.CCID("chaincode-id-name:chaincode-version"), ccid)
}

func TestContainerRuntimeStopErrors(t *testing.T) {
	tests := []struct {
		processErr    error
		containerType string
		errValue      string
	}{
		{errors.New("process-failed"), "DOCKER", "error stopping container: process-failed"},
		{nil, "BADVM", "unknown container type: BADVM"},
	}

	for _, tc := range tests {
		fakeVM := &mock.ContainerVM{}
		fakeVM.StopReturns(tc.processErr)

		cr := &chaincode.ContainerRuntime{
			VMSynchronizer: container.NewVMController(map[string]container.VM{
				"DOCKER": fakeVM,
			}),
			PeerAddress: "peer.example.com",
		}

		ccci := &ccprovider.ChaincodeContainerInfo{
			Type:          pb.ChaincodeSpec_GOLANG.String(),
			Name:          "chaincode-id-name",
			Version:       "chaincode-version",
			ContainerType: tc.containerType,
		}

		assert.EqualError(t, cr.Stop(ccci), tc.errValue)
	}
}

func TestContainerRuntimeWait(t *testing.T) {
	fakeVM := &mock.ContainerVM{}

	cr := &chaincode.ContainerRuntime{
		VMSynchronizer: container.NewVMController(map[string]container.VM{
			"DOCKER": fakeVM,
		}),
		PeerAddress: "peer.example.com",
	}

	ccci := &ccprovider.ChaincodeContainerInfo{
		Type:          pb.ChaincodeSpec_GOLANG.String(),
		Name:          "chaincode-id-name",
		Version:       "chaincode-version",
		ContainerType: "DOCKER",
		PackageID:     persistence.PackageID("chaincode-id-name:chaincode-version"),
	}

	exitCode, err := cr.Wait(ccci)
	assert.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Equal(t, 1, fakeVM.WaitCallCount())
	assert.Equal(t, ccintf.CCID("chaincode-id-name:chaincode-version"), fakeVM.WaitArgsForCall(0))

	fakeVM.WaitReturns(3, errors.New("moles-and-trolls"))
	code, err := cr.Wait(ccci)
	assert.EqualError(t, err, "moles-and-trolls")
	assert.Equal(t, code, 3)
}
