package main

const macOSDeploymentTarget = "14.0"

func darwinEnvironment() []string {
	return []string{
		"MACOSX_DEPLOYMENT_TARGET=" + macOSDeploymentTarget,
		"CGO_CFLAGS=-O2 -Werror=unguarded-availability-new",
	}
}
