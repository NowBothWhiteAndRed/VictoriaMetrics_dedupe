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

	samples := []pushSample{
		{key: "foo", value: 1, timestamp: 1000, insertTimestamp: 10},
		{key: "foo", value: 2, timestamp: 1001, insertTimestamp: 11},
		{key: "foo", value: 3, timestamp: 1002, insertTimestamp: 11},
		{key: "foo", value: 0, timestamp: 1000, insertTimestamp: 12},
	}
	da.pushSamples(samples, 0, false)

	var flushed []pushSample
	da.flush(func(ss []pushSample, _ int64, _ bool) {
		flushed = append(flushed, ss...)
	}, time.Now().UnixMilli(), false)

	if len(flushed) != 1 {
		t.Fatalf("unexpected flushed samples: %d", len(flushed))
	}
	s := flushed[0]
	if s.key != "foo" || s.value != 0 || s.timestamp != 1000 || s.insertTimestamp != 12 {
		t.Fatalf("unexpected sample %+v", s)
	}
}
