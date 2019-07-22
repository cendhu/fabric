/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package container

import (
	"sync"

	"github.com/hyperledger/fabric/common/flogging"
	"github.com/hyperledger/fabric/core/common/ccprovider"
	"github.com/hyperledger/fabric/core/container/ccintf"
)

var vmLogger = flogging.MustGetLogger("container")

//VM is an abstract virtual image for supporting arbitrary virual machines
type VM interface {
	Build(ccci *ccprovider.ChaincodeContainerInfo, codePackage []byte) error
	Start(ccid ccintf.CCID, ccType string, env []string, filesToUpload map[string][]byte) error
	Stop(ccid ccintf.CCID) error
	Wait(ccid ccintf.CCID) (int, error)
}

type LockingVM struct {
	Underlying     VM
	ContainerLocks *ContainerLocks
}

func (lvm *LockingVM) Build(ccci *ccprovider.ChaincodeContainerInfo, codePackage []byte) error {
	ccid := ccintf.CCID(ccci.PackageID)
	lvm.ContainerLocks.Lock(ccid)
	defer lvm.ContainerLocks.Unlock(ccid)
	return lvm.Underlying.Build(ccci, codePackage)
}

func (lvm *LockingVM) Start(ccid ccintf.CCID, ccType string, env []string, filesToUpload map[string][]byte) error {
	lvm.ContainerLocks.Lock(ccid)
	defer lvm.ContainerLocks.Unlock(ccid)
	return lvm.Underlying.Start(ccid, ccType, env, filesToUpload)
}

func (lvm *LockingVM) Stop(ccid ccintf.CCID) error {
	lvm.ContainerLocks.Lock(ccid)
	defer lvm.ContainerLocks.Unlock(ccid)
	return lvm.Underlying.Stop(ccid)
}

func (lvm *LockingVM) Wait(ccid ccintf.CCID) (int, error) {
	// There is a race here, that was previously masked by the fact that
	// the callback was called after the lock had been released, so
	// unlocking before blocking
	lvm.ContainerLocks.Lock(ccid)
	waitFunc := lvm.Underlying.Wait
	lvm.ContainerLocks.Unlock(ccid)
	return waitFunc(ccid)
}

type ContainerLocks struct {
	mutex          sync.RWMutex
	containerLocks map[ccintf.CCID]*refCountedLock
}

func NewContainerLocks() *ContainerLocks {
	return &ContainerLocks{
		containerLocks: make(map[ccintf.CCID]*refCountedLock),
	}
}

func (cl *ContainerLocks) Lock(id ccintf.CCID) {
	//get the container lock under global lock
	cl.mutex.Lock()
	var refLck *refCountedLock
	var ok bool
	if refLck, ok = cl.containerLocks[id]; !ok {
		refLck = &refCountedLock{refCount: 1, lock: &sync.RWMutex{}}
		cl.containerLocks[id] = refLck
	} else {
		refLck.refCount++
		vmLogger.Debugf("refcount %d (%s)", refLck.refCount, id)
	}
	cl.mutex.Unlock()
	vmLogger.Debugf("waiting for container(%s) lock", id)
	refLck.lock.Lock()
	vmLogger.Debugf("got container (%s) lock", id)
}

func (cl *ContainerLocks) Unlock(id ccintf.CCID) {
	cl.mutex.Lock()
	if refLck, ok := cl.containerLocks[id]; ok {
		if refLck.refCount <= 0 {
			panic("refcnt <= 0")
		}
		refLck.lock.Unlock()
		if refLck.refCount--; refLck.refCount == 0 {
			vmLogger.Debugf("container lock deleted(%s)", id)
			delete(cl.containerLocks, id)
		}
	} else {
		vmLogger.Debugf("no lock to unlock(%s)!!", id)
	}
	cl.mutex.Unlock()
}

type refCountedLock struct {
	refCount int
	lock     *sync.RWMutex
}
