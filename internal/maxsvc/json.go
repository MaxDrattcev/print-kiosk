package maxsvc

func PrintSessionJSON(sess *PrintSession) map[string]any {
	files := make([]map[string]any, 0, len(sess.Files))
	remaining := 0
	for _, f := range sess.Files {
		if !f.Printed {
			remaining++
		}
		files = append(files, map[string]any{
			"id": f.ID, "name": f.Name, "size": f.Size, "printed": f.Printed,
		})
	}
	deadlineMS := int64(0)
	if !sess.Deadline.IsZero() {
		deadlineMS = sess.Deadline.UnixMilli()
	}
	return map[string]any{
		"id": sess.ID, "status": sess.Status, "error": sess.Error,
		"from": sess.From, "files": files, "remaining": remaining,
		"deadline_ms": deadlineMS,
	}
}

func ScanSessionJSON(sess *ScanSession) map[string]any {
	deadlineMS := int64(0)
	if !sess.Deadline.IsZero() {
		deadlineMS = sess.Deadline.UnixMilli()
	}
	return map[string]any{
		"id": sess.ID, "job_id": sess.JobID, "code": sess.Code,
		"status": sess.Status, "error": sess.Error,
		"deadline_ms": deadlineMS,
	}
}
