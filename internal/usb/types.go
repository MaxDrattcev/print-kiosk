package usb

type Drive struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Label string `json:"label"`
}

type Entry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size"`
	Ext   string `json:"ext"`
}
