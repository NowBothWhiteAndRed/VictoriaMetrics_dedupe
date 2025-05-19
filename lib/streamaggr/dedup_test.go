package streamaggr

import (
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestDedupAggrSerial(t *testing.T) {
	da := newDedupAggr(false)

	const seriesCount = 100_000
	expectedSamplesMap := make(map[string]pushSample)
	samples := make([]pushSample, seriesCount)
	for j := range samples {
		sample := &samples[j]
		sample.key = fmt.Sprintf("key_%d", j)
		sample.value = float64(j)
		expectedSamplesMap[sample.key] = *sample
	}
	da.pushSamples(samples, 0, false)

	if n := da.sizeBytes(); n > 5_000_000 {
		t.Fatalf("too big dedupAggr state before flush: %d bytes; it shouldn't exceed 5_000_000 bytes", n)
	}
	if n := da.itemsCount(); n != seriesCount {
		t.Fatalf("unexpected itemsCount; got %d; want %d", n, seriesCount)
	}

	flushedSamplesMap := make(map[string]pushSample)
	var mu sync.Mutex
	flushSamples := func(samples []pushSample, _ int64, _ bool) {
		mu.Lock()
		for _, sample := range samples {
			flushedSamplesMap[sample.key] = sample
		}
		mu.Unlock()
	}

	flushTimestamp := time.Now().UnixMilli()
	da.flush(flushSamples, flushTimestamp, false)

	if !reflect.DeepEqual(expectedSamplesMap, flushedSamplesMap) {
		t.Fatalf("unexpected samples;\ngot\n%v\nwant\n%v", flushedSamplesMap, expectedSamplesMap)
	}

	if n := da.sizeBytes(); n > 17_000 {
		t.Fatalf("too big dedupAggr state after flush; %d bytes; it shouldn't exceed 17_000 bytes", n)
	}
	if n := da.itemsCount(); n != 0 {
		t.Fatalf("unexpected non-zero itemsCount after flush; got %d", n)
	}
}

func TestDedupAggrConcurrent(_ *testing.T) {
	const concurrency = 5
	const seriesCount = 10_000
	da := newDedupAggr(false)

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				samples := make([]pushSample, seriesCount)
				for j := range samples {
					sample := &samples[j]
					sample.key = fmt.Sprintf("key_%d", j)
					sample.value = float64(i + j)
				}
				da.pushSamples(samples, 0, false)
			}
		}()
	}
	wg.Wait()
}

func TestDedupAggrInsertTimestamp(t *testing.T) {
	da := newDedupAggr(true)
	base := time.Now().UnixMilli()

	da.pushSamples([]pushSample{{
		key:             "foo",
		value:           1,
		timestamp:       100,
		insertTimestamp: base,
	}}, 0, false)

	da.pushSamples([]pushSample{{
		key:             "foo",
		value:           2,
		timestamp:       80,
		insertTimestamp: base + 10,
	}}, 0, false)

	var got []pushSample
	da.flush(func(samples []pushSample, _ int64, _ bool) {
		got = append(got, samples...)
	}, base+20, false)

	if len(got) != 1 {
		t.Fatalf("unexpected samples count: %d", len(got))
	}
	s := got[0]
	if s.value != 2 || s.timestamp != 80 || s.insertTimestamp != base+10 {
		t.Fatalf("unexpected sample after deduplication: %+v", s)
	}
}
