package matrixhandoff

const (
	ContentVideo   = "video"
	ContentArticle = "article"
	ContentCover   = "cover"
)

const (
	FixtureVideoOnly = "fixture.video_only"
	FixtureArticle   = "fixture.article"
	FixtureCover     = "fixture.cover"
)

// Platform 描述一个夹具平台能收的内容类型。Fixture 为真不是 Service PASS。
type Platform struct {
	ID           string
	ContentTypes []string
	Fixture      bool
}

// Allows 报告该平台是否声明了这种内容类型。
func (p Platform) Allows(contentType string) bool {
	for _, item := range p.ContentTypes {
		if item == contentType {
			return true
		}
	}
	return false
}

// VideoOnly 只收视频，不收文章或封面。
func (p Platform) VideoOnly() bool {
	return p.Allows(ContentVideo) && !p.Allows(ContentArticle) && !p.Allows(ContentCover)
}

// Catalog 是平台内容类型目录。夹具目录不是在线矩阵。
type Catalog struct {
	byID map[string]Platform
}

// NewFixtureCatalog 返回本包内置的三个夹具平台。
func NewFixtureCatalog() *Catalog {
	c := &Catalog{byID: map[string]Platform{}}
	c.put(Platform{ID: FixtureVideoOnly, ContentTypes: []string{ContentVideo}, Fixture: true})
	c.put(Platform{ID: FixtureArticle, ContentTypes: []string{ContentArticle}, Fixture: true})
	c.put(Platform{ID: FixtureCover, ContentTypes: []string{ContentCover}, Fixture: true})
	return c
}

func (c *Catalog) put(p Platform) {
	copied := p
	copied.ContentTypes = append([]string(nil), p.ContentTypes...)
	c.byID[p.ID] = copied
}

// Platform 按编号取出平台。未知编号不是在线服务。
func (c *Catalog) Platform(id string) (Platform, bool) {
	if c == nil {
		return Platform{}, false
	}
	p, ok := c.byID[id]
	if !ok {
		return Platform{}, false
	}
	p.ContentTypes = append([]string(nil), p.ContentTypes...)
	return p, true
}
