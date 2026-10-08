# The look of every recording: 85×22 cells at 2x, shown at 776px. A cell is
# 16.8 by 37.4 pixels. With the padding, W cells wide and H rows tall is
# Width 16.8×W+118 and Height 37.4×H+89, rounded up to even. The padding is
# in the video, so the frame's screen is all video and shows no seam.
# record.sh adds the theme from theme.json, and a tape may set its own
# Width and Height. See docs/design/docs-site-look.md.
Set Shell "bash"
Set FontFamily "JetBrains Mono"
Set FontSize 28
Set LineHeight 1.0
Set Width 1552
Set Height 912
Set Padding 36
Set TypingSpeed 35ms
Set CursorBlink false
Set WaitPattern /\$\s*$/
Set WaitTimeout 90s
