package store

import "time"

// Expiry is enforced on reads and also removes the temporary encrypted record.
// Other task results and unexpired credentials must survive maintenance.
const expiredCredential = `CASE WHEN json_valid(result_json) THEN
  json_type(result_json,'$.credential')='object' AND
  (COALESCE(json_type(result_json,'$.credential.expiresAt'),'')!='integer'
   OR json_extract(result_json,'$.credential.expiresAt')<=?)
  ELSE 0 END`

func (s *Store) ClearExpiredCredentials(taskID string, now time.Time) error {
	_, err := s.DB.Exec(`UPDATE task_items SET result_json='' WHERE task_id=? AND `+expiredCredential, taskID, now.Unix())
	return err
}

func (s *Store) PruneAuthentication(now time.Time) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM sessions WHERE expires_at<=?`, now.Unix()); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE task_items SET result_json='' WHERE result_json!='' AND `+expiredCredential, now.Unix()); err != nil {
		return err
	}
	return tx.Commit()
}
