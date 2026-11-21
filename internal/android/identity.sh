# The system images of the emulator all share one serial number.
avd=$(getprop ro.boot.qemu.avd_name)
if [ "$(getprop ro.boot.qemu)" = 1 ] && [ -n "$avd" ]; then
	echo "emulator:$avd"
	echo "$avd"
else
	getprop ro.serialno
	name=$(settings get global device_name 2>/dev/null)
	if [ -z "$name" ] || [ "$name" = null ]; then
		name=$(getprop ro.product.model)
	fi
	echo "$name"
fi
