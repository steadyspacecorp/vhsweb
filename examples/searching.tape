# Search interaction with lazy-loaded results, exported as a silent GIF.
#
#   python3 -m http.server 8080 --directory examples
#   vhsweb examples/searching.tape
#
# Exercises: Goto, Scroll, Click, Type, Press, WaitFor, Screenshot.
# GIF output is always silent.

Output searching.gif
Set Width 1280
Set Height 720
Set Zoom 1
Set TypingSpeed 70ms

Goto http://localhost:8080/demo-page.html
Sleep 600ms

# Scroll the search section into view and focus the input.
Scroll Down 1100
Sleep 800ms
Click "#query"
Type "edge"
Press Enter

# Results inject after a ~1.2s delay.
WaitFor "#results"
Sleep 500ms
Screenshot searching.png
Sleep 1500ms
