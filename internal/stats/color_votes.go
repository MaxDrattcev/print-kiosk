package stats

import "fmt"

func (r *Repo) VoteForColorPrint(sessionID, source string) error {
	if source != "print" && source != "copy" {
		return fmt.Errorf("неизвестный источник голоса")
	}
	_, err := r.db.Exec(`INSERT OR IGNORE INTO color_print_votes(session_id,source) VALUES (?,?)`, sessionID, source)
	return err
}
func (r *Repo) ColorPrintVotes() (int64, error) {
	var count int64
	err := r.db.QueryRow(`SELECT COUNT(*) FROM color_print_votes`).Scan(&count)
	return count, err
}
