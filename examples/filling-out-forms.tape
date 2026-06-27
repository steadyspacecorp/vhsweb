# Fill out the signup form on the local demo page.
#
#   python3 -m http.server 8080 --directory examples
#   vhsweb examples/filling-out-forms.tape
#
# Exercises: Goto, Click, Fill, Type, Press, WaitFor.

Output filling-out-forms.mp4
Set Width 1280
Set Height 720
Set Zoom 1
Set TypingSpeed 45ms
Set Sound true

Goto http://localhost:8080/demo-page.html
Sleep 800ms

# Land on the signup section.
Click "#get-started"
Sleep 1s

# Fill sets a value instantly; Type plays out keystroke by keystroke (with sound).
#Fill "#name" "Ada Lovelace"
Click "#name"
Type "Ada Lovelace"
Sleep 500ms
Click "#email"
Type "ada@example.com"
Sleep 600ms
Click "#create-account"

# The success banner appears after submit.
WaitFor "#welcome"
Sleep 2s
