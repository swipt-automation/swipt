package main

// The oldest macOS that the macOS app supports (MACOSX_DEPLOYMENT_TARGET in
// Swipt.xcodeproj). Without it, the driver requires the macOS of the SDK it is
// built with.
const macOSDeploymentTarget = "14.0"

func darwinEnvironment() []string {
	return []string{
		"MACOSX_DEPLOYMENT_TARGET=" + macOSDeploymentTarget,
		"CGO_CFLAGS=-O2 -Werror=unguarded-availability-new",
	}
}
