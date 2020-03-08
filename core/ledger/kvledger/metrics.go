/*
Copyright IBM Corp. All Rights Reserved.
SPDX-License-Identifier: Apache-2.0
*/

package kvledger

import (
	"time"

	"github.com/hyperledger/fabric/common/metrics"
	"github.com/hyperledger/fabric/core/ledger/kvledger/txmgmt/txmgr"
)

type stats struct {
	blockProcessingTime            metrics.Histogram
	blockAndPvtdataStoreCommitTime metrics.Histogram
	statedbCommitTime              metrics.Histogram
	transactionsCount              metrics.Counter
	historydbCommitTime            metrics.Histogram
	cacheHitEndorsement            metrics.Counter
	cacheMissEndorsement           metrics.Counter
	cacheHitCommit                 metrics.Counter
	cacheMissCommit                metrics.Counter
	cacheCollisions                metrics.Counter
	cacheSize                      metrics.Counter
}

func newStats(metricsProvider metrics.Provider) *stats {
	stats := &stats{}
	stats.blockProcessingTime = metricsProvider.NewHistogram(blockProcessingTimeOpts)
	stats.blockAndPvtdataStoreCommitTime = metricsProvider.NewHistogram(blockAndPvtdataStoreCommitTimeOpts)
	stats.statedbCommitTime = metricsProvider.NewHistogram(statedbCommitTimeOpts)
	stats.transactionsCount = metricsProvider.NewCounter(transactionCountOpts)
	stats.historydbCommitTime = metricsProvider.NewHistogram(historydbCommitTimeOpts)
	stats.cacheHitEndorsement = metricsProvider.NewCounter(cacheHitEndorsementOpts)
	stats.cacheMissEndorsement = metricsProvider.NewCounter(cacheMissEndorsementOpts)
	stats.cacheHitCommit = metricsProvider.NewCounter(cacheHitCommitOpts)
	stats.cacheMissCommit = metricsProvider.NewCounter(cacheMissCommitOpts)
	stats.cacheCollisions = metricsProvider.NewCounter(cacheCollisionsOpts)
	stats.cacheSize = metricsProvider.NewCounter(cacheSizeOpts)
	return stats
}

type ledgerStats struct {
	stats    *stats
	ledgerid string
}

func (s *stats) ledgerStats(ledgerid string) *ledgerStats {
	return &ledgerStats{
		s, ledgerid,
	}
}

func (s *ledgerStats) updateBlockProcessingTime(timeTaken time.Duration) {
	s.stats.blockProcessingTime.With("channel", s.ledgerid).Observe(timeTaken.Seconds())
}

func (s *ledgerStats) updateBlockstorageAndPvtdataCommitTime(timeTaken time.Duration) {
	s.stats.blockAndPvtdataStoreCommitTime.With("channel", s.ledgerid).Observe(timeTaken.Seconds())
}

func (s *ledgerStats) updateStatedbCommitTime(timeTaken time.Duration) {
	s.stats.statedbCommitTime.With("channel", s.ledgerid).Observe(timeTaken.Seconds())
}

func (s *ledgerStats) updateHistorydbCommitTime(timeTaken time.Duration) {
	s.stats.historydbCommitTime.With("channel", s.ledgerid).Observe(timeTaken.Seconds())
}

func (s *ledgerStats) updateCacheMetrics(m ...uint64) {
	s.stats.cacheHitEndorsement.With("channel", s.ledgerid).Add(float64(m[0]))
	s.stats.cacheMissEndorsement.With("channel", s.ledgerid).Add(float64(m[1]))
	s.stats.cacheHitCommit.With("channel", s.ledgerid).Add(float64(m[2]))
	s.stats.cacheMissCommit.With("channel", s.ledgerid).Add(float64(m[3]))
	s.stats.cacheCollisions.With("channel", s.ledgerid).Add(float64(m[4]))
	s.stats.cacheSize.With("channel", s.ledgerid).Add(float64(m[5]))
}

func (s *ledgerStats) updateTransactionsStats(
	txstatsInfo []*txmgr.TxStatInfo,
) {
	for _, txstat := range txstatsInfo {
		transactionTypeStr := "unknown"
		if txstat.TxType != -1 {
			transactionTypeStr = txstat.TxType.String()
		}

		chaincodeName := "unknown"
		if txstat.ChaincodeID != nil {
			chaincodeName = txstat.ChaincodeID.Name + ":" + txstat.ChaincodeID.Version
		}

		s.stats.transactionsCount.With(
			"channel", s.ledgerid,
			"transaction_type", transactionTypeStr,
			"chaincode", chaincodeName,
			"validation_code", txstat.ValidationCode.String(),
		).Add(1)
	}
}

var (
	blockProcessingTimeOpts = metrics.HistogramOpts{
		Namespace:    "ledger",
		Subsystem:    "",
		Name:         "block_processing_time",
		Help:         "Time taken in seconds for ledger block processing.",
		LabelNames:   []string{"channel"},
		StatsdFormat: "%{#fqname}.%{channel}",
		Buckets:      []float64{0.005, 0.01, 0.015, 0.05, 0.1, 1, 10},
	}

	blockAndPvtdataStoreCommitTimeOpts = metrics.HistogramOpts{
		Namespace:    "ledger",
		Subsystem:    "",
		Name:         "blockstorage_and_pvtdata_commit_time",
		Help:         "Time taken in seconds for committing the block and private data to storage.",
		LabelNames:   []string{"channel"},
		StatsdFormat: "%{#fqname}.%{channel}",
		Buckets:      []float64{0.005, 0.01, 0.015, 0.05, 0.1, 1, 10},
	}

	statedbCommitTimeOpts = metrics.HistogramOpts{
		Namespace:    "ledger",
		Subsystem:    "",
		Name:         "statedb_commit_time",
		Help:         "Time taken in seconds for committing block changes to state db.",
		LabelNames:   []string{"channel"},
		StatsdFormat: "%{#fqname}.%{channel}",
		Buckets:      []float64{0.005, 0.01, 0.015, 0.05, 0.1, 1, 10},
	}

	transactionCountOpts = metrics.CounterOpts{
		Namespace:    "ledger",
		Subsystem:    "",
		Name:         "transaction_count",
		Help:         "Number of transactions processed.",
		LabelNames:   []string{"channel", "transaction_type", "chaincode", "validation_code"},
		StatsdFormat: "%{#fqname}.%{channel}.%{transaction_type}.%{chaincode}.%{validation_code}",
	}

	cacheHitEndorsementOpts = metrics.CounterOpts{
		Namespace:    "ledger",
		Subsystem:    "",
		Name:         "cache_hit_endorsement_count",
		Help:         "Number of transactions processed.",
		LabelNames:   []string{"channel"},
		StatsdFormat: "%{#fqname}.%{channel}",
	}

	cacheMissEndorsementOpts = metrics.CounterOpts{
		Namespace:    "ledger",
		Subsystem:    "",
		Name:         "cache_miss_endorsement_count",
		Help:         "Number of transactions processed.",
		LabelNames:   []string{"channel"},
		StatsdFormat: "%{#fqname}.%{channel}",
	}

	cacheHitCommitOpts = metrics.CounterOpts{
		Namespace:    "ledger",
		Subsystem:    "",
		Name:         "cache_hit_commit_count",
		Help:         "Number of transactions processed.",
		LabelNames:   []string{"channel"},
		StatsdFormat: "%{#fqname}.%{channel}",
	}

	cacheMissCommitOpts = metrics.CounterOpts{
		Namespace:    "ledger",
		Subsystem:    "",
		Name:         "cache_miss_commit_count",
		Help:         "Number of transactions processed.",
		LabelNames:   []string{"channel"},
		StatsdFormat: "%{#fqname}.%{channel}",
	}

	cacheCollisionsOpts = metrics.CounterOpts{
		Namespace:    "ledger",
		Subsystem:    "",
		Name:         "cache_collisions_count",
		Help:         "Number of transactions processed.",
		LabelNames:   []string{"channel"},
		StatsdFormat: "%{#fqname}.%{channel}",
	}

	cacheSizeOpts = metrics.CounterOpts{
		Namespace:    "ledger",
		Subsystem:    "",
		Name:         "cache_size",
		Help:         "Number of transactions processed.",
		LabelNames:   []string{"channel"},
		StatsdFormat: "%{#fqname}.%{channel}",
	}

	historydbCommitTimeOpts = metrics.HistogramOpts{
		Namespace:    "ledger",
		Subsystem:    "",
		Name:         "historydb_commit_time",
		Help:         "Time taken in seconds for committing block changes to state db.",
		LabelNames:   []string{"channel"},
		StatsdFormat: "%{#fqname}.%{channel}",
		Buckets:      []float64{0.005, 0.01, 0.015, 0.05, 0.1, 1, 10},
	}
)
