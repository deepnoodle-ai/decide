#!/bin/sh
# Records the site's terminal recordings and uploads them to R2.
#
#   site/scripts/record.sh                 record and upload each tape not uploaded yet
#   site/scripts/record.sh landing         only these tapes
#   site/scripts/record.sh --local landing record into site/tapes/out, to try with DECIDE_MEDIA
#
# An uploaded file is cached for a year and never replaced. To record a tape
# again, change it; a comment is enough. The hash leaves out this script, so
# a change to the hidden setup or the encoding below waits for the next
# release, or for a change to each tape.
#
# A recording uploads only if its last frame has every "# Expect:" line of
# its tape, and no "Error:" line.
#
# Each tape in site/tapes becomes <tape>-<hash>.mp4, .png and .txt (and any
# GIF the tape asks for) under decide/site/ in the deepnoodle-public bucket,
# which serves files.deepnoodle.ai. The hash covers the tape, settings.tape,
# theme.json and the release in plugin.json; src/lib/tapes.ts computes the
# same one. VHS runs in Docker, so every recording has the same fonts.
#
# Needs Docker, Go, Node, TYPESAFE_API_KEY, and a Cloudflare login that can
# write to the bucket (npx wrangler login, in site/).
set -eu

site=$(cd "$(dirname "$0")/.." && pwd)
repo=$(cd "$site/.." && pwd)
tapes="$site/tapes"
base=https://files.deepnoodle.ai/decide/site
bucket=deepnoodle-public/decide/site
: "${CLOUDFLARE_ACCOUNT_ID:=776aacf24c324e7bb825561ff1b47038}"
export CLOUDFLARE_ACCOUNT_ID

local=false
while [ $# -gt 0 ]; do
	case $1 in
	--local) local=true ;;
	-*) echo "unknown flag $1" >&2; exit 1 ;;
	*) break ;;
	esac
	shift
done
if [ -z "${TYPESAFE_API_KEY:-}" ]; then
	echo "Set TYPESAFE_API_KEY: recordings ask Jev for real answers." >&2
	exit 1
fi

version=$(node -p "require('$repo/plugin/.claude-plugin/plugin.json').version")
hash() {
	{ cat "$tapes/$1.tape" "$tapes/settings.tape" "$tapes/theme.json"; printf '%s' "$version"; } |
		shasum -a 256 | cut -c1-12
}

if [ $# -gt 0 ]; then
	names=$*
else
	names=$(cd "$tapes" && ls *.tape | sed 's/\.tape$//' | grep -v '^settings$')
fi
if ! $local; then
	missing=
	for name in $names; do
		if curl -sfI "$base/$name-$(hash "$name").txt" >/dev/null; then
			echo "$name is already uploaded. To record it again, change the tape."
		else
			missing="$missing $name"
		fi
	done
	names=$missing
fi
if [ -z "$(echo $names)" ]; then
	echo "Every tape is recorded."
	exit 0
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
arch=$(docker version --format '{{.Server.Arch}}')
(cd "$repo" && GOOS=linux GOARCH=$arch CGO_ENABLED=0 go build -o "$work/bin/decide" ./cmd/decide)
docker build -q -t decide-vhs - < "$site/scripts/vhs.Dockerfile" >/dev/null
theme=$(tr -d '\n\t' < "$tapes/theme.json")

for name in $names; do
	tape="$tapes/$name.tape"
	[ -f "$tape" ] || { echo "no tape $tape" >&2; exit 1; }
	h=$(hash "$name")
	echo "Recording $name ($h)"
	# Settings first, then the tape's own Output and Set lines, then a clean
	# shell with no saved answers, then the tape.
	{
		echo "Output $name.mp4"
		echo "Output $name.txt"
		echo "Set Theme $theme"
		cat "$tapes/settings.tape"
		grep -E '^(Output|Set) ' "$tape" || true
		echo 'Hide'
		echo "Type \"export DECIDE_HOME=\$(mktemp -d) PS1='\$ ' && cd \$(mktemp -d) && clear\" Enter"
		echo 'Wait'
		echo 'Show'
		grep -vE '^(Output|Set) ' "$tape"
	} > "$work/$name.tape"
	docker run --rm -e TYPESAFE_API_KEY -e REPO=/repo -w /work \
		-v "$work:/work" -v "$repo:/repo:ro" -v "$work/bin/decide:/usr/local/bin/decide:ro" \
		decide-vhs "$name.tape" >"$work/$name.log" 2>&1 || { cat "$work/$name.log" >&2; exit 1; }
	# Encode with tagged BT.709 color, so browsers draw the background as
	# the frame's #121316, and so it plays before the whole file arrives.
	# The poster is the last frame. (VHS's Screenshot sometimes writes nothing.)
	docker run --rm -w /work -v "$work:/work" --entrypoint sh decide-vhs -c "
		ffmpeg -loglevel error -i $name.mp4 -vf scale=out_color_matrix=bt709:out_range=tv \
			-c:v libx264 -preset slow -tune animation -crf 20 -pix_fmt yuv420p \
			-colorspace bt709 -color_primaries bt709 -color_trc bt709 -color_range tv \
			-movflags +faststart $name.fast.mp4 &&
		mv $name.fast.mp4 $name.mp4 &&
		ffmpeg -loglevel error -sseof -0.2 -i $name.mp4 -update 1 -frames:v 1 $name.png"
	# The last frame's text, without the blank rows and the prompt below it.
	awk '/^─+$/ { last = cur; cur = ""; next } { cur = cur $0 "\n" } END { printf "%s", last }' "$work/$name.txt" |
		awk '{ line[NR] = $0 } END { n = NR; while (n > 0 && line[n] ~ /^(\$ *)?$/) n--; for (i = 1; i <= n; i++) print line[i] }' \
			> "$work/$name.last.txt"
	mv "$work/$name.last.txt" "$work/$name.txt"
	# A provider error or an answer that changed must not reach the site.
	bad=
	if grep -q '^Error:' "$work/$name.txt"; then bad="it shows an error"; fi
	sed -n 's/^# Expect: //p' "$tape" > "$work/$name.expect"
	while IFS= read -r want; do
		grep -qF -- "$want" "$work/$name.txt" || bad="${bad:+$bad; }it lacks \"$want\""
	done < "$work/$name.expect"
	if [ -n "$bad" ]; then
		echo "Not uploading $name: $bad. Its last frame:" >&2
		cat "$work/$name.txt" >&2
		exit 1
	fi

	for file in "$work/$name".*; do
		ext=${file##*.}
		case $ext in
		mp4) type=video/mp4 ;; png) type=image/png ;; gif) type=image/gif ;;
		txt) type='text/plain; charset=utf-8' ;; *) continue ;;
		esac
		if $local; then
			mkdir -p "$tapes/out"
			cp "$file" "$tapes/out/$name-$h.$ext"
		else
			(cd "$site" && npx wrangler r2 object put "$bucket/$name-$h.$ext" --remote \
				--file "$file" --content-type "$type" \
				--cache-control 'public, max-age=31536000, immutable' >/dev/null)
			echo "  $base/$name-$h.$ext"
		fi
	done
done
