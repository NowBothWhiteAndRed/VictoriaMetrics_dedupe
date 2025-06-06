package streamaggr

import (
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/VictoriaMetrics/metrics"
	"github.com/cespare/xxhash/v2"

	"github.com/VictoriaMetrics/VictoriaMetrics/lib/bytesutil"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/decimal"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/slicesutil"
)

const dedupAggrShardsCount = 128

type dedupAggr struct {
	shards             []dedupAggrShard
	flushDuration      *metrics.Histogram
	flushTimeouts      *metrics.Counter
	useInsertTimestamp bool
}

type dedupAggrShard struct {
	dedupAggrShardNopad

	// The padding prevents false sharing on widespread platforms with
	// 128 mod (cache line size) = 0 .
	_ [128 - unsafe.Sizeof(dedupAggrShardNopad{})%128]byte
}

type dedupAggrState struct {
	m          map[string]*dedupAggrSample
	mu         sync.Mutex
	samplesBuf []dedupAggrSample
	sizeBytes  atomic.Uint64
	itemsCount atomic.Uint64
}

type dedupAggrShardNopad struct {
	blue  dedupAggrState
	green dedupAggrState
}

type dedupAggrSample struct {
	value           float64
	timestamp       int64
	insertTimestamp int64
}

func newDedupAggr(useInsertTs bool) *dedupAggr {
	return &dedupAggr{
		shards:             make([]dedupAggrShard, dedupAggrShardsCount),
		useInsertTimestamp: useInsertTs,
	}
}

func (da *dedupAggr) sizeBytes() uint64 {
	n := uint64(unsafe.Sizeof(*da))
	var shard *dedupAggrShard
	for i := range da.shards {
		shard = &da.shards[i]
		n += shard.blue.sizeBytes.Load()
		n += shard.green.sizeBytes.Load()
	}
	return n
}

func (da *dedupAggr) itemsCount() uint64 {
	n := uint64(0)
	var shard *dedupAggrShard
	for i := range da.shards {
		shard = &da.shards[i]
		n += shard.blue.itemsCount.Load()
		n += shard.green.itemsCount.Load()
	}
	return n
}

func (da *dedupAggr) pushSamples(samples []pushSample, _ int64, isGreen bool) {
	pss := getPerShardSamples()
	shards := pss.shards
	for _, sample := range samples {
		h := xxhash.Sum64(bytesutil.ToUnsafeBytes(sample.key))
		idx := h % uint64(len(shards))
		shards[idx] = append(shards[idx], sample)
	}
	for i, shardSamples := range shards {
		if len(shardSamples) == 0 {
			continue
		}
		da.shards[i].pushSamples(shardSamples, isGreen, da.useInsertTimestamp)
	}
	putPerShardSamples(pss)
}

func getDedupFlushCtx(deleteDeadline int64, isGreen bool) *dedupFlushCtx {
	v := dedupFlushCtxPool.Get()
	if v == nil {
		v = &dedupFlushCtx{}
	}
	ctx := v.(*dedupFlushCtx)
	ctx.deleteDeadline = deleteDeadline
	ctx.isGreen = isGreen
	return ctx
}

func putDedupFlushCtx(ctx *dedupFlushCtx) {
	ctx.reset()
	dedupFlushCtxPool.Put(ctx)
}

var dedupFlushCtxPool sync.Pool

type dedupFlushCtx struct {
	samples        []pushSample
	deleteDeadline int64
	isGreen        bool
}

func (ctx *dedupFlushCtx) reset() {
	clear(ctx.samples)
	ctx.samples = ctx.samples[:0]
	ctx.deleteDeadline = 0
}

func (da *dedupAggr) flush(f aggrPushFunc, deleteDeadline int64, isGreen bool) {
	var wg sync.WaitGroup
	for i := range da.shards {
		flushConcurrencyCh <- struct{}{}
		wg.Add(1)
		go func(shard *dedupAggrShard) {
			defer func() {
				<-flushConcurrencyCh
				wg.Done()
			}()
			ctx := getDedupFlushCtx(deleteDeadline, isGreen)
			shard.flush(ctx, f)
			putDedupFlushCtx(ctx)
		}(&da.shards[i])
	}
	wg.Wait()
}

type perShardSamples struct {
	shards [][]pushSample
}

func (pss *perShardSamples) reset() {
	shards := pss.shards
	for i, shardSamples := range shards {
		if len(shardSamples) > 0 {
			clear(shardSamples)
			shards[i] = shardSamples[:0]
		}
	}
}

func getPerShardSamples() *perShardSamples {
	v := perShardSamplesPool.Get()
	if v == nil {
		return &perShardSamples{
			shards: make([][]pushSample, dedupAggrShardsCount),
		}
	}
	return v.(*perShardSamples)
}

func putPerShardSamples(pss *perShardSamples) {
	pss.reset()
	perShardSamplesPool.Put(pss)
}

var perShardSamplesPool sync.Pool

func (das *dedupAggrShard) pushSamples(samples []pushSample, isGreen bool, useInsertTs bool) {
	var state *dedupAggrState

	if isGreen {
		state = &das.green
	} else {
		state = &das.blue
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	if state.m == nil {
		state.m = make(map[string]*dedupAggrSample, len(samples))
	}
	samplesBuf := state.samplesBuf
	for _, sample := range samples {
		s, ok := state.m[sample.key]
		if !ok {
			samplesBuf = slicesutil.SetLength(samplesBuf, len(samplesBuf)+1)
			s = &samplesBuf[len(samplesBuf)-1]
			s.value = sample.value
			s.timestamp = sample.timestamp
			s.insertTimestamp = sample.insertTimestamp

			key := bytesutil.InternString(sample.key)
			state.m[key] = s

			state.itemsCount.Add(1)
			state.sizeBytes.Add(uint64(len(key)) + uint64(unsafe.Sizeof(key)+unsafe.Sizeof(s)+unsafe.Sizeof(*s)))
			continue
		}
		if !isDuplicate(s, sample, useInsertTs) {
			s.value = sample.value
			s.timestamp = sample.timestamp
			s.insertTimestamp = sample.insertTimestamp
		}
	}
	state.samplesBuf = samplesBuf
}

// isDuplicate returns true if b is duplicate of a
// See https://docs.victoriametrics.com/victoriametrics/single-server-victoriametrics/#deduplication
func isDuplicate(a *dedupAggrSample, b pushSample, useInsertTs bool) bool {
	if useInsertTs {
		if b.insertTimestamp > a.insertTimestamp {
			return false
		}
		if b.insertTimestamp == a.insertTimestamp {
			if b.timestamp > a.timestamp {
				return false
			}
			if b.timestamp == a.timestamp {
				if decimal.IsStaleNaN(b.value) {
					return false
				}
				if b.value > a.value {
					return false
				}
			}
		}
		return true
	}

	if b.timestamp > a.timestamp {
		return false
	}
	if b.timestamp == a.timestamp {
		if decimal.IsStaleNaN(b.value) {
			return false
		}
		if b.value > a.value {
			return false
		}
	}
	return true
}

func (das *dedupAggrShard) flush(ctx *dedupFlushCtx, f aggrPushFunc) {
	var m map[string]*dedupAggrSample
	var state *dedupAggrState
	if ctx.isGreen {
		state = &das.green
	} else {
		state = &das.blue
	}

	state.mu.Lock()
	if len(state.m) > 0 {
		m = state.m
		state.m = make(map[string]*dedupAggrSample, len(state.m))
		state.samplesBuf = make([]dedupAggrSample, 0, len(state.samplesBuf))
		state.sizeBytes.Store(0)
		state.itemsCount.Store(0)
	}
	state.mu.Unlock()

	if len(m) == 0 {
		return
	}

	dstSamples := ctx.samples
	for key, s := range m {
		dstSamples = append(dstSamples, pushSample{
			key:             key,
			value:           s.value,
			timestamp:       s.timestamp,
			insertTimestamp: s.insertTimestamp,
		})

		// Limit the number of samples per each flush in order to limit memory usage.
		if len(dstSamples) >= 10_000 {
			f(dstSamples, ctx.deleteDeadline, false)
			clear(dstSamples)
			dstSamples = dstSamples[:0]
		}
	}
	f(dstSamples, ctx.deleteDeadline, false)
	ctx.samples = dstSamples
}
