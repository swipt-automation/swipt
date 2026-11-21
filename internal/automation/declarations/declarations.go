package declarations

import "regexp"

var Modules = func() []string {
	var modules []string
	for _, match := range regexp.MustCompile(`(?m)^declare module "([^"]+)"`).FindAllStringSubmatch(TypeScript, -1) {
		modules = append(modules, match[1])
	}
	return modules
}()
