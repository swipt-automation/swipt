//go:build cgo

package automation

type OS string

const (
	IOS     OS = "ios"
	Android OS = "android"
)

func (os OS) String() string {
	switch os {
	case IOS:
		return "iOS"
	case Android:
		return "Android"
	}
	return string(os)
}

type Environment struct {
	OSVersion string
	Languages []string
}
