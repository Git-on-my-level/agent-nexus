package series

import (
	"context"
	"testing"
	"time"
)

func TestTimelinePreservesNearbyObservationsAndBounds(t *testing.T) {
	s, _, _, _ := fixture(t)
	now := time.Now().UTC()
	if _, err := s.DB.Exec(`INSERT INTO series_labels(series,labels) VALUES('health','{}')`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4100; i++ {
		if _, err := s.DB.Exec(`INSERT INTO series_points(series,labels,ts,state,received_day) VALUES('health','{}',?,'release',0)`, now.Add(-time.Duration(i)*time.Millisecond).UnixNano()); err != nil {
			t.Fatal(err)
		}
	}
	result, truncated, err := s.Timeline(context.Background(), "health", nil, time.Hour, now)
	if err != nil || !truncated || len(result.Streams) != 1 || len(result.Streams[0].Points) != 100 {
		t.Fatalf("%#v %v %v", result, truncated, err)
	}
	if result.Streams[0].Points[0].TS != now.Format(time.RFC3339Nano) || result.Streams[0].Points[1].TS != now.Add(-time.Millisecond).Format(time.RFC3339Nano) {
		t.Fatal("nearby releases were bucketed or reordered")
	}
	if _, _, err := s.Timeline(context.Background(), "health", nil, Retention+time.Hour, now); err == nil {
		t.Fatal("accepted timeline beyond raw retention")
	}
}
