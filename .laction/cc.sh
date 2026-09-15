# Install a C compiler if missing. Bookworm images already have gcc.
# Alpine leftover: apk add build-base. Debian: apt-get gcc libc6-dev.

ensure_cc() {
	if command -v cc >/dev/null 2>&1 || command -v gcc >/dev/null 2>&1; then
		return 0
	fi
	if [ -f /etc/alpine-release ]; then
		apk add --no-cache build-base
		return 0
	fi
	if command -v apt-get >/dev/null 2>&1; then
		apt-get update -qq
		DEBIAN_FRONTEND=noninteractive apt-get install -y -qq gcc libc6-dev >/dev/null
		return 0
	fi
	return 1
}
