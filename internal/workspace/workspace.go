package workspace

// Stub for workspace management
type Workspace struct{}

type Content struct {
Path   string
Data   []byte
Soul   string
Agents string
Tools  string
Skill  string
}

func (c *Content) IsEmpty() bool {
return c == nil || (c.Soul == "" && c.Agents == "" && c.Tools == "" && c.Skill == "")
}

func New(path string) *Workspace {
return &Workspace{}
}

func Load(path string) (*Content, error) {
return &Content{Path: path}, nil
}

type Edit struct {
Path    string
Content string
}
