package module


type ApiResp struct {
	Errno  int64       `json:"errno"`
	Errmsg string      `json:"errmsg"`
	Data   interface{} `json:"data,omitempty"`
}