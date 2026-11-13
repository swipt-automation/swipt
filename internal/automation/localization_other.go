//go:build cgo && !darwin

package automation

import (
	"slices"

	"golang.org/x/text/language"
)

func preferredLocalizations(localizations []string, preferences []string) []string {
	if len(localizations) == 0 {
		return []string{}
	}
	tags := make([]language.Tag, len(localizations))
	for index, localization := range localizations {
		tags[index], _ = language.Parse(localization)
	}
	matcher := language.NewMatcher(tags)
	for _, preference := range preferences {
		tag, err := language.Parse(preference)
		if err != nil {
			continue
		}
		_, index, confidence := matcher.Match(tag)
		if confidence == language.No {
			continue
		}
		result := []string{localizations[index]}
		for parent := tags[index].Parent(); parent != language.Und; parent = parent.Parent() {
			if found := slices.Index(tags, parent); found >= 0 && !slices.Contains(result, localizations[found]) {
				result = append(result, localizations[found])
			}
		}
		return result
	}
	return localizations[:1]
}
