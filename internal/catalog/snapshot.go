package catalog

import (
	"time"

	"nekopic/internal/model"
)

// Snapshot 是一次扫描得到的图片清单，生成后不再修改；查询时按值返回单条记录。
// dm 不是真实分类：它代表全部分类的合并，按配置顺序串在一起随机抽取，
// 保证每张图片被抽中的机会相同。
type Snapshot struct {
	order      []string
	categories map[string][]model.ImageInfo
	byPath     map[string]model.ImageInfo
	Version    uint64
	ScannedAt  time.Time
}

// Order 返回分类名的配置顺序。
func (s *Snapshot) Order() []string {
	if s == nil {
		return nil
	}
	return s.order
}

func (s *Snapshot) Count(category string) int {
	if s == nil {
		return 0
	}
	if category == "dm" {
		total := 0
		for _, name := range s.order {
			total += len(s.categories[name])
		}
		return total
	}
	return len(s.categories[category])
}

func (s *Snapshot) At(category string, index int) model.ImageInfo {
	if s == nil {
		return model.ImageInfo{}
	}
	if category == "dm" {
		for _, name := range s.order {
			list := s.categories[name]
			if index < len(list) {
				return list[index]
			}
			index -= len(list)
		}
		return model.ImageInfo{}
	}
	list := s.categories[category]
	if index < 0 || index >= len(list) {
		return model.ImageInfo{}
	}
	return list[index]
}

func (s *Snapshot) Lookup(path string) (model.ImageInfo, bool) {
	if s == nil {
		return model.ImageInfo{}, false
	}
	img, ok := s.byPath[path]
	return img, ok
}
