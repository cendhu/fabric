/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package chaincode

import (
	"sync"

	pb "github.com/hyperledger/fabric-protos-go/peer"
	commonledger "github.com/hyperledger/fabric/common/ledger"
	"github.com/hyperledger/fabric/core/common/ccprovider"
	"github.com/pkg/errors"
)

// TransactionContexts maintains active transaction contexts for a Handler.
type TransactionContexts struct {
	mutex    sync.Mutex
	contexts map[string]*TransactionContext
}

// NewTransactionContexts creates a registry for active transaction contexts.
func NewTransactionContexts() *TransactionContexts {
	return &TransactionContexts{
		contexts: map[string]*TransactionContext{},
	}
}

// contextID creates a transaction identifier that is scoped to a chain.
func contextID(chainID, txID string) string {
	return chainID + txID
}

// Create creates a new TransactionContext for the specified chain and
// transaction ID. An error is returned when a transaction context has already
// been created for the specified chain and transaction ID.
func (c *TransactionContexts) Create(txParams *ccprovider.TransactionParams) (*TransactionContext, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	ctxID := contextID(txParams.ChannelID, txParams.TxID)
	if c.contexts[ctxID] != nil {
		return nil, errors.Errorf("txid: %s(%s) exists", txParams.TxID, txParams.ChannelID)
	}

	txctx := &TransactionContext{
		NamespaceID:          txParams.NamespaceID,
		ChainID:              txParams.ChannelID,
		SignedProp:           txParams.SignedProp,
		Proposal:             txParams.Proposal,
		ResponseNotifier:     make(chan *pb.ChaincodeMessage, 1),
		TXSimulator:          txParams.TXSimulator,
		HistoryQueryExecutor: txParams.HistoryQueryExecutor,
		CollectionStore:      txParams.CollectionStore,
		IsInitTransaction:    txParams.IsInitTransaction,

		queryIteratorMap:    map[string]commonledger.ResultsIterator{},
		pendingQueryResults: map[string]*PendingQueryResult{},
	}
	txctx.InitializeCollectionACLCache()

	c.contexts[ctxID] = txctx

	return txctx, nil
}

// Get retrieves the transaction context associated with the chain and
// transaction ID.
func (c *TransactionContexts) Get(chainID, txID string) *TransactionContext {
	ctxID := contextID(chainID, txID)
	c.mutex.Lock()
	tc := c.contexts[ctxID]
	c.mutex.Unlock()
	return tc
}

// Delete removes the transaction context associated with the specified chain
// and transaction ID.
func (c *TransactionContexts) Delete(chainID, txID string) {
	ctxID := contextID(chainID, txID)
	c.mutex.Lock()
	delete(c.contexts, ctxID)
	c.mutex.Unlock()
}

// Close closes all query iterators assocated with the context.
func (c *TransactionContexts) Close() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	for _, txctx := range c.contexts {
		txctx.CloseQueryIterators()
	}
}
