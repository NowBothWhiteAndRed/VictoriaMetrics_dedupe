package streamaggr

import (
	"testing"
	"time"

	"github.com/VictoriaMetrics/VictoriaMetrics/lib/prompbmarshal"
)

func BenchmarkDeduplicatorPush(b *testing.B) {
	pushFunc := func(_ []prompbmarshal.TimeSeries) {}
	d := NewDeduplicator(pushFunc, true, time.Hour, nil, "global", false)

	b.ReportAllocs()
	b.SetBytes(int64(len(benchSeries)))
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			d.Push(benchSeries)
		}
	})
}
