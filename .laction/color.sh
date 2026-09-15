# Color helpers. Laction captures stdout as a pipe, so echo '\033[...'
# prints garbage. Use printf with a real ESC, and only when stdout is a TTY
# (or FORCE_COLOR=1). Honor NO_COLOR.
#
# Usage: . "$(dirname "$0")/color.sh"   then   info "text"

ESC=$(printf '\033')
BLUE="" GREEN="" CYAN="" MAGENTA="" YELLOW="" RED="" NC=""

if [ -n "$NO_COLOR" ]; then
	:
elif [ -t 1 ] || [ "$FORCE_COLOR" = "1" ]; then
	BLUE="${ESC}[34m"
	GREEN="${ESC}[32m"
	CYAN="${ESC}[36m"
	MAGENTA="${ESC}[35m"
	YELLOW="${ESC}[33m"
	RED="${ESC}[31m"
	NC="${ESC}[0m"
fi

info() { printf '%s===> %s%s\n' "$BLUE" "$*" "$NC"; }
ok() { printf '%s✓ %s%s\n' "$GREEN" "$*" "$NC"; }
warn() { printf '%s===> %s%s\n' "$YELLOW" "$*" "$NC"; }
note() { printf '%s===> %s%s\n' "$CYAN" "$*" "$NC"; }
hdr() { printf '%s===> %s%s\n' "$MAGENTA" "$*" "$NC"; }
