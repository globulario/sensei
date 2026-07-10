#!/usr/bin/env python3
"""Render The_Art_of_Software_Architecture.md to a styled PDF matching the
reference edition: title page, running header, serif body, maroon headings,
shaded field-note boxes, page numbers, and working internal links.

markdown -> HTML (python-markdown) -> WeasyPrint (CSS paged media).

WeasyPrint is installed in a local venv; run this script with that interpreter,
e.g.  /tmp/pdfvenv/bin/python docs/article/build_pdf.py
"""
import re
from pathlib import Path

import markdown
from weasyprint import HTML, CSS as WeasyCSS

HERE = Path(__file__).resolve().parent
MD = HERE / "The_Art_of_Software_Architecture.md"
PDF = HERE / "The_Art_of_Software_Architecture.pdf"

CSS = """
@page {
  size: A4; margin: 22mm 20mm 18mm 20mm;
  @top-center {
    content: "THE ART OF SOFTWARE ARCHITECTURE";
    font-family: Georgia, serif; font-size: 7.5pt; letter-spacing: 1px;
    color: #9a8f82; padding-bottom: 3mm;
  }
  @bottom-center {
    content: counter(page); font-family: Georgia, serif;
    font-size: 8.5pt; color: #7a7269; padding-top: 3mm;
  }
}
@page :first { @top-center { content: none; } @bottom-center { content: none; } }

body {
  font-family: Georgia, "Liberation Serif", "Times New Roman", serif;
  font-size: 10.5pt; line-height: 1.5; color: #1c2229;
  text-align: justify; hyphens: auto;
}

/* Title page: everything before the first <hr>. */
.titlepage { page-break-after: always; text-align: center; padding-top: 40mm; }
.titlepage h1 {
  font-size: 30pt; line-height: 1.15; color: #2b3440;
  border: none; margin: 0 0 8mm 0; letter-spacing: .5px; page-break-after: avoid;
}
.titlepage h3 {
  color: #7a3b2e; font-style: italic; font-weight: normal;
  font-size: 14pt; border: none; margin: 0 0 24mm 0;
}
.titlepage p { text-align: center; color: #333; font-size: 11pt; margin: 5mm 16mm; }
.titlepage blockquote {
  border: none !important; background: none !important; color: #7a3b2e;
  font-style: italic; font-size: 12.5pt; margin: 30mm 14mm 0 14mm;
  padding: 0; text-align: center;
}

h1, h2, h3, h4 { font-family: Georgia, "Liberation Serif", serif; page-break-after: avoid; }

/* Chapter / section titles. */
h2 {
  color: #7a3b2e; font-size: 18pt; margin: 10mm 0 3mm 0;
  padding-bottom: 2mm; border-bottom: 1.5px solid #d8cfc4;
  page-break-before: always;
}

/* Maxim titles. */
h4 { color: #8a4636; font-size: 12pt; font-weight: bold; margin: 6mm 0 1mm 0;
     page-break-after: avoid; }

/* Severity tag: the lone italic line right after a maxim title. */
h4 + p { color: #a0968a; font-size: 8pt; letter-spacing: .6px;
         text-transform: uppercase; margin: 0 0 1.5mm 0; }
h4 + p em { font-style: normal; }

p { margin: 2.2mm 0; }
strong { color: #2b3440; }
a { color: #7a3b2e; text-decoration: none; }
ul { margin: 2mm 0 3mm 0; padding-left: 7mm; }
li { margin: 1mm 0; }
hr { border: none; border-top: 1px solid #e2dbd0; margin: 6mm 0; }
code { font-family: "DejaVu Sans Mono", monospace; font-size: 8.5pt;
       background: #f4f1ec; padding: 0 2px; color: #52443a; }

/* Field notes: chapter-closing blockquotes. */
blockquote {
  background: #f3efe8; border-left: 3px solid #b98a5e;
  margin: 6mm 2mm; padding: 3mm 6mm; color: #5b5147; font-style: italic;
  page-break-inside: avoid;
}
/* Chapter intro paragraph (italic, right after the h2). */
h2 + p em { color: #4a4139; }

/* Keep a maxim's title + severity + trap together where possible. */
h4 { orphans: 3; widows: 3; }
"""


def build_html(md_text: str) -> str:
    html = markdown.markdown(
        md_text,
        extensions=["extra", "sane_lists", "attr_list", "smarty"],
        output_format="html5",
    )
    parts = html.split("<hr />", 1)
    if len(parts) == 2:
        html = f'<div class="titlepage">{parts[0]}</div>\n{parts[1]}'
    return f"<!DOCTYPE html><html><head><meta charset='utf-8'></head><body>{html}</body></html>"


def main() -> None:
    html = build_html(MD.read_text())
    HTML(string=html, base_url=str(HERE)).write_pdf(
        str(PDF), stylesheets=[WeasyCSS(string=CSS)]
    )
    print(f"Wrote {PDF} ({PDF.stat().st_size // 1024} KB)")


if __name__ == "__main__":
    main()
