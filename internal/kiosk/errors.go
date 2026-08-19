package kiosk

type errString string

func (e errString) Error() string { return string(e) }
