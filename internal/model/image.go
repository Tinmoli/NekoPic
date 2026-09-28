package model

import "time"

type ImageInfo struct {
	Category  string
	Filename  string
	Width     int
	Height    int
	Extension string
	Format    string
	Bytes     int64
	ModTime   time.Time
	PublicURL string
}

type ImageResponse struct {
	Code   string `json:"code"`
	AcgURL string `json:"acgurl"`
	Width  string `json:"width"`
	Height string `json:"height"`
	Size   string `json:"size"`
}
