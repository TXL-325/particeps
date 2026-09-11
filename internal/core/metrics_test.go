package core

import "testing"

func TestMetricsMigrationPreservesHistoryAndNetworkGaps(t *testing.T) {
	a, _ := testApp(t)
	if _, err := a.Metrics.DB.Exec(`DROP TABLE samples;
		CREATE TABLE samples(ts INTEGER,object TEXT,cpu_cores REAL,rx_bps REAL,tx_bps REAL,quality TEXT,quota REAL);
		INSERT INTO samples VALUES(1,'host',1,2,3,'ok',2)`); err != nil {
		t.Fatal(err)
	}
	if err := initMetrics(a.Metrics.DB); err != nil {
		t.Fatal(err)
	}
	if err := initMetrics(a.Metrics.DB); err != nil {
		t.Fatal(err)
	}
	_, err := a.Metrics.DB.Exec(`INSERT INTO samples(ts,object,cpu_cores,rx_bps,tx_bps,quality,network_quality,quota) VALUES(2,'host',1,?,?,'ok','missing',2)`, validRate(0, "missing"), validRate(0, "missing"))
	if err != nil {
		t.Fatal(err)
	}
	series, err := a.Series("host", "", "")
	if err != nil || len(series) != 2 {
		t.Fatalf("history: %+v %v", series, err)
	}
	if series[0]["networkQuality"] != "ok" || series[0]["rxBps"] != float64(2) {
		t.Fatalf("legacy sample lost: %+v", series[0])
	}
	if series[1]["quality"] != "ok" || series[1]["quotaPercent"] != float64(50) || series[1]["rxBps"] != nil || series[1]["networkQuality"] != "missing" {
		t.Fatalf("missing network obscured CPU or became a valid zero: %+v", series[1])
	}
}
