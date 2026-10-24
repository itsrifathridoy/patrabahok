patrabahok skin for Roundcube
=============================

The default look of patrabahok Webmail: a modern, branded theme for the Roundcube
engine, installed by `lib/phases/90-webmail.sh`.

It **extends** Roundcube's Elastic skin (`"extends": "elastic"` in `meta.json`):
templates and assets that aren't overridden here fall back to Elastic, so every
Roundcube feature keeps working, and Roundcube upgrades keep the theme intact. What it
changes:

- `styles/patrabahok.css` — the design system (colors, type, radius, spacing) and the
  restyled mail list, reading pane, composer, settings, contacts and dialogs, in light
  and dark mode. Loaded after Elastic's stylesheet by `templates/includes/layout.html`.
- `templates/login.html` — branded sign-in page.
- `images/` — patrabahok logos.

The new features (undo send, command palette, keyboard shortcuts, avatars, composer
tools) are in the companion plugin, `../../plugins/patrabahok`.

LICENSE
-------

This skin is a derivative of Roundcube's Elastic skin by Aleksander Machniak, and like
Elastic it is licensed under the Creative Commons Attribution-ShareAlike 3.0 License
(http://creativecommons.org/licenses/by-sa/3.0/). This differs from the MIT license of
the rest of the patrabahok repository.

Bundled font: Plus Jakarta Sans (SIL Open Font License 1.1, `fonts/OFL.txt`).
