# Example tapes

A few `.tape` scripts to try vhsweb out. Build the binary first
(`go build -o vhsweb .` from the repo root) and run `./vhsweb install` once.

## Against the bundled demo page

`demo-page.html` is a self-contained fake product page ("Nimbus") built to
exercise every command: a hover dropdown, CTA buttons, a signup form, and a
search box that lazy-loads results. Serve the folder, then run a tape:

```sh
python3 -m http.server 8080 --directory examples
./vhsweb examples/browsing.tape     # nav hover, click, scroll        -> browsing.mp4
./vhsweb examples/filling-out-forms.tape   # fill / type / press / waitfor  -> filling-out-forms.mp4
./vhsweb examples/searching.tape   # search + lazy results, silent   -> searching.gif
```

(Any static server works — `npx serve examples`, `ruby -run -e httpd examples`,
etc. The tapes expect the page at `http://localhost:8080/demo-page.html`.)

Add `--preview` to watch any tape run live in a real browser window without
recording — handy for tuning selectors and timing:

```sh
./vhsweb --preview examples/browsing.tape
```

| Tape | Output | Commands shown |
| --- | --- | --- |
| `browsing.tape` | mp4 | `Goto` `Hover` `Click` `Scroll` `WaitFor` `Sleep` |
| `filling-out-forms.tape` | mp4 | `Fill` `Type` `Press` `Click` `WaitFor` |
| `searching.tape` | gif | `Type` `Press` `WaitFor` `Screenshot` |
