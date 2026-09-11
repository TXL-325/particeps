package core

import "database/sql"

func initMetrics(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS samples (
		ts INTEGER NOT NULL, object TEXT NOT NULL, cpu_cores REAL,
		rx_bps REAL, tx_bps REAL, quality TEXT, network_quality TEXT, quota REAL
	); CREATE INDEX IF NOT EXISTS samples_obj_ts ON samples(object,ts)`); err != nil {
		return err
	}
	rows, err := db.Query(`PRAGMA table_info(samples)`)
	if err != nil {
		return err
	}
	hasNetworkQuality := false
	for rows.Next() {
		var cid, required, primary int
		var name, kind string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &kind, &required, &defaultValue, &primary); err != nil {
			rows.Close()
			return err
		}
		hasNetworkQuality = hasNetworkQuality || name == "network_quality"
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if !hasNetworkQuality {
		_, err = db.Exec(`ALTER TABLE samples ADD COLUMN network_quality TEXT`)
	}
	return err
}

func validRate(value float64, quality string) any {
	if quality != "ok" {
		return nil
	}
	return value
}

func nullableRate(value sql.NullFloat64) any {
	if !value.Valid {
		return nil
	}
	return value.Float64
}
