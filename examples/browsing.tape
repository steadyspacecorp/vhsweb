# A full tour of the local demo page (examples/demo-page.html).
#
# First serve the examples folder, then run this tape:
#   python3 -m http.server 8080 --directory examples
#   vhsweb examples/browsing.tape
#
# Exercises: Goto, Sleep, Hover, Click, Scroll, WaitFor.

Output browsing.mp4
Set Width 1280
Set Height 720
Set Zoom 1
Set Framerate 30
Set Sound true

Goto http://localhost:8080/demo-page.html
Sleep 1s

# Open the nav dropdown by hovering the Products menu.
Hover "#products-menu"
Sleep 1500ms

# Jump to the features section and let the cards settle.
Click "#learn-more"
Sleep 1s
Hover ".card[data-card='safe']"
Sleep 1500ms

# Scroll down through the page.
Scroll Down 700
Sleep 1500ms
Scroll Down 700
Sleep 2s
