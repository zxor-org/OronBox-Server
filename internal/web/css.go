package web

const CSS = `
/* Material 3 Expressive tokens.
   Dark is the baseline because that is how the console is actually used; light
   is an override rather than the other way round. */
:root {
  color-scheme: dark;

  /* Colour roles */
  --primary: #d4bbff;
  --on-primary: #3a1d70;
  --primary-container: #523a8c;
  --on-primary-container: #ebddff;
  --secondary: #ccc2dc;
  --secondary-container: #4a4458;
  --on-secondary-container: #e8def8;
  --tertiary: #f0b8c8;
  --tertiary-container: #663b49;
  --on-tertiary-container: #ffd9e2;
  --surface: #131017;
  --on-surface: #e7e0e8;
  --on-surface-variant: #cbc3d2;
  --surface-lowest: #0d0b11;
  --surface-low: #1a171f;
  --surface-container: #201d26;
  --surface-high: #2b2732;
  --surface-highest: #362f3e;
  --surface-container-low: var(--surface-low);
  --surface-container-high: var(--surface-high);
  --outline: #968fa1;
  --outline-variant: #453f4d;
  --error: #f2b8b5;
  --on-error: #601410;
  --error-container: #7d211c;
  --on-error-container: #ffdad6;
  --success: #7fe08d;
  --success-container: #10531f;
  --warning: #f7cd7a;
  --warning-container: #5c4200;
  --info: #c2ccf5;
  --info-container: #3a4468;
  --scrim: rgb(0 0 0 / .58);

  /* Shape scale. Expressive leans on larger, less uniform radii than the
     baseline, which is what keeps a dense console from reading as a grid of
     identical boxes. */
  --shape-xs: 6px;
  --shape-sm: 10px;
  --shape-md: 14px;
  --shape-lg: 20px;
  --shape-xl: 28px;
  --shape-full: 999px;

  /* Type scale */
  --type-display: 400 32px/1.2 var(--font-sans);
  --type-headline: 400 24px/1.3 var(--font-sans);
  --type-title-lg: 500 20px/1.35 var(--font-sans);
  --type-title: 500 16px/1.4 var(--font-sans);
  --type-body: 400 14px/1.55 var(--font-sans);
  --type-body-sm: 400 13px/1.5 var(--font-sans);
  --type-label: 500 13px/1.4 var(--font-sans);
  --type-label-sm: 500 11px/1.35 var(--font-sans);
  --tracking-label: .06em;

  /* Form metrics. Controls share one height so a select, an input and a button
     line up on the same row without per-page nudging, and one field width so
     filter columns keep a rhythm instead of sizing themselves to their labels. */
  --control-height: 40px;
  --control-pad: 12px;
  --field-gap: 6px;
  --field-min: 190px;
  --form-gap: 14px;

  /* Motion. The emphasized curve accelerates late, which is the expressive
     part; the standard curve stays calm for things that move constantly. */
  --ease-emphasized: cubic-bezier(.2, 0, 0, 1);
  --ease-standard: cubic-bezier(.4, 0, .2, 1);
  --duration-short: 120ms;
  --duration-medium: 220ms;
  --duration-long: 380ms;

  /* Elevation */
  --elevation-1: 0 1px 2px rgb(0 0 0 / .4), 0 1px 3px 1px rgb(0 0 0 / .22);
  --elevation-2: 0 2px 6px 2px rgb(0 0 0 / .22), 0 1px 2px rgb(0 0 0 / .4);
  --elevation-3: 0 12px 34px rgb(0 0 0 / .42);
  --shadow-2: var(--elevation-3);

  /* State layer opacities, straight from the spec */
  --state-hover: .08;
  --state-focus: .1;
  --state-pressed: .1;

  --header-height: 56px;
  --drawer-width: 260px;
  --spacing: 16px;
  --font-sans: system-ui, -apple-system, "Segoe UI", "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", Roboto, sans-serif;
  --font-mono: ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace;
  font-family: var(--font-sans);
}

/* Light overrides, applied when the operator asks for it or the system does. */
:root.light-theme {
  color-scheme: light;
  --primary: #6750a4;
  --on-primary: #ffffff;
  --primary-container: #eaddff;
  --on-primary-container: #21005d;
  --secondary: #625b71;
  --secondary-container: #e8def8;
  --on-secondary-container: #1d192b;
  --tertiary: #7d5260;
  --tertiary-container: #ffd8e4;
  --on-tertiary-container: #31111d;
  --surface: #fdf7ff;
  --on-surface: #1c1b20;
  --on-surface-variant: #494551;
  --surface-lowest: #ffffff;
  --surface-low: #f7f2fa;
  --surface-container: #f2ecf6;
  --surface-high: #ebe5f0;
  --surface-highest: #e4dee9;
  --outline: #78737f;
  --outline-variant: #cac4d0;
  --error: #b3261e;
  --on-error: #ffffff;
  --error-container: #f9dedc;
  --on-error-container: #410e0b;
  --success: #146c2e;
  --success-container: #c4eece;
  --warning: #7a5900;
  --warning-container: #ffdea5;
  --info: #455a7a;
  --info-container: #dae2ff;
  --scrim: rgb(0 0 0 / .4);
  --elevation-1: 0 1px 2px rgb(0 0 0 / .1), 0 1px 3px 1px rgb(0 0 0 / .07);
  --elevation-2: 0 2px 6px 2px rgb(0 0 0 / .08), 0 1px 2px rgb(0 0 0 / .12);
  --elevation-3: 0 10px 32px rgb(0 0 0 / .16);
}

@media (prefers-color-scheme: light) {
  :root:not(.dark-theme):not(.light-theme) {
    color-scheme: light;
    --primary: #6750a4;
    --on-primary: #ffffff;
    --primary-container: #eaddff;
    --on-primary-container: #21005d;
    --secondary: #625b71;
    --secondary-container: #e8def8;
    --on-secondary-container: #1d192b;
    --tertiary: #7d5260;
    --tertiary-container: #ffd8e4;
    --on-tertiary-container: #31111d;
    --surface: #fdf7ff;
    --on-surface: #1c1b20;
    --on-surface-variant: #494551;
    --surface-lowest: #ffffff;
    --surface-low: #f7f2fa;
    --surface-container: #f2ecf6;
    --surface-high: #ebe5f0;
    --surface-highest: #e4dee9;
    --outline: #78737f;
    --outline-variant: #cac4d0;
    --error: #b3261e;
    --on-error: #ffffff;
    --error-container: #f9dedc;
    --on-error-container: #410e0b;
    --success: #146c2e;
    --success-container: #c4eece;
    --warning: #7a5900;
    --warning-container: #ffdea5;
    --info: #455a7a;
    --info-container: #dae2ff;
    --scrim: rgb(0 0 0 / .4);
    --elevation-1: 0 1px 2px rgb(0 0 0 / .1), 0 1px 3px 1px rgb(0 0 0 / .07);
    --elevation-2: 0 2px 6px 2px rgb(0 0 0 / .08), 0 1px 2px rgb(0 0 0 / .12);
    --elevation-3: 0 10px 32px rgb(0 0 0 / .16);
  }
}

* { box-sizing: border-box; }
.sr-only {
  position: absolute !important;
  width: 1px !important;
  height: 1px !important;
  padding: 0 !important;
  margin: -1px !important;
  overflow: hidden !important;
  clip: rect(0, 0, 0, 0) !important;
  white-space: nowrap !important;
  border: 0 !important;
}
html { min-width: 320px; min-height: 100%; background: var(--surface-low); }
body {
  margin: 0;
  height: 100vh;
  height: 100dvh;
  overflow: hidden;
  background: var(--surface-low);
  color: var(--on-surface);
  font: var(--type-body);
  -webkit-font-smoothing: antialiased;
}
body.drawer-open { overflow: hidden; }
a { color: var(--primary); text-decoration: none; }
button, input, select, textarea { font: inherit; }
button, a { -webkit-tap-highlight-color: transparent; }
/* Form controls are deliberately absent here: they draw their own ring against
   their own border in the forms section, and stacking this outline on top of it
   gave every focused field two concentric rings. */
button:focus-visible, a:focus-visible, summary:focus-visible {
  outline: 3px solid color-mix(in srgb, var(--primary) 42%, transparent);
  outline-offset: 2px;
}
code, pre { font-family: var(--font-mono); }
code { font-size: 12px; overflow-wrap: anywhere; }
h1, h2, h3, p { margin-top: 0; }
h1 { font: var(--type-headline); letter-spacing: -.01em; }
h2 { font: var(--type-title); }
h3 { font: var(--type-label); }
p { color: var(--on-surface-variant); }
.icon-sprite { display: none; }
.icon {
  width: var(--icon-size, 22px);
  height: var(--icon-size, 22px);
  flex: 0 0 auto;
  fill: currentColor;
  vertical-align: -.15em;
  pointer-events: none;
}
.skip-link {
  position: fixed;
  z-index: 1000;
  top: 8px;
  left: 8px;
  padding: 8px 14px;
  border-radius: var(--shape-full);
  background: var(--primary);
  color: var(--on-primary);
  transform: translateY(-160%);
}
.skip-link:focus { transform: translateY(0); }

.app-header {
  position: relative;
  z-index: 700;
  height: var(--header-height);
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 16px;
  border-bottom: 1px solid var(--outline-variant);
  background: var(--surface);
}
.header-section { display: flex; align-items: center; gap: 8px; }
.header-brand {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 0 8px;
  color: var(--on-surface);
  font-size: 18px;
  font-weight: 500;
}
.brand-badge {
  padding: 2px 7px;
  border-radius: var(--shape-xs);
  background: var(--secondary-container);
  color: var(--on-secondary-container);
  font-size: 10px;
  font-weight: 700;
  letter-spacing: .08em;
}
.icon-button {
  position: relative;
  isolation: isolate;
  overflow: hidden;
  width: 40px;
  height: 40px;
  display: inline-grid;
  place-items: center;
  padding: 0;
  border: 0;
  border-radius: var(--shape-full);
  background: transparent;
  color: var(--on-surface-variant);
  cursor: pointer;
}
.icon-button:hover { color: var(--on-surface); }
.header-logout { margin: 0; display: flex; }
.admin-layout {
  height: calc(100vh - var(--header-height));
  height: calc(100dvh - var(--header-height));
  display: flex;
  overflow: hidden;
}
.nav {
  position: relative;
  z-index: 600;
  flex: 0 0 var(--drawer-width);
  width: var(--drawer-width);
  padding: 12px 8px;
  overflow-y: auto;
  border-right: 1px solid var(--outline-variant);
  background: var(--surface);
  transition: transform .3s cubic-bezier(.4, 0, .2, 1);
}
.nav-content { display: flex; flex-direction: column; gap: 2px; }
.nav-group-label {
  margin: 16px 16px 4px;
  color: var(--outline);
  font-size: 11px;
  font-weight: 700;
  letter-spacing: .08em;
}
.nav-group-label:first-child { margin-top: 4px; }
.nav-link {
  height: 48px;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 0 16px;
  border: 0;
  border-radius: var(--shape-full);
  background: transparent;
  color: var(--on-surface-variant);
  font: var(--type-label);
  font-size: 14px;
  text-align: left;
  cursor: pointer;
  transition: background var(--duration-medium) var(--ease-emphasized), color var(--duration-short) var(--ease-standard);
}
.nav-link { position: relative; isolation: isolate; overflow: hidden; }
.nav-link:hover { color: var(--on-surface); }
.nav-link.active {
  background: var(--secondary-container);
  color: var(--on-secondary-container);
  border-start-end-radius: var(--shape-sm);
  border-end-end-radius: var(--shape-sm);
  font-weight: 600;
}
.nav-link.active .icon { color: var(--primary); }
.drawer-overlay { display: none; }
.admin-main {
  flex: 1;
  min-width: 0;
  min-height: 0;
  margin-left: 0;
  padding: var(--spacing);
  overflow-y: auto;
  scroll-behavior: smooth;
  background: var(--surface-low);
}
body.drawer-collapsed .nav { margin-left: calc(-1 * var(--drawer-width)); }

.page-header {
  min-height: 36px;
  margin: 0 0 var(--spacing);
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}
.page-header h1, .page-title { margin: 0; color: var(--on-surface); font: var(--type-headline); letter-spacing: -.01em; }
.page-header p { margin: 4px 0 0; font-size: 13px; }
.count-badge {
  display: inline-flex;
  align-items: center;
  min-height: 28px;
  padding: 2px 12px;
  border-radius: var(--shape-full);
  background: var(--secondary-container);
  color: var(--on-secondary-container);
  font: var(--type-label);
  font-size: 12px;
  white-space: nowrap;
}
.back-link {
  display: inline-flex;
  align-items: center;
  margin-bottom: 6px;
  font-size: 13px;
  font-weight: 500;
}
.title-line { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; }

.panel, .review-card, .ticket-card {
  min-width: 0;
  margin-bottom: var(--spacing);
  padding: 20px;
  border: 1px solid var(--outline-variant);
  border-radius: var(--shape-lg);
  background: var(--surface);
}
.section-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 12px;
}
.section-header h2 { margin: 0; color: var(--primary); }
.section-header p { margin: 3px 0 0; font-size: 12px; }
.content-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--spacing); }
.content-grid > .panel { margin-bottom: 0; }
.dashboard-grid { grid-template-columns: minmax(0, 1.5fr) minmax(300px, 1fr); }
.span-2 { grid-column: 1 / -1; }
.settings-grid, .detail-grid { align-items: start; }

.metrics {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: 12px;
  margin-bottom: var(--spacing);
}
.metrics.system-metrics { grid-template-columns: repeat(4, minmax(150px, 1fr)); }
.metrics article {
  min-width: 0;
  padding: 16px 18px;
  border: 1px solid var(--outline-variant);
  border-radius: var(--shape-lg);
  background: var(--surface);
  transition: border-color var(--duration-short) var(--ease-standard);
}
.metrics article:hover { border-color: var(--outline); }
.metrics span, .metrics small { display: block; color: var(--on-surface-variant); }
.metrics span { font: var(--type-label-sm); letter-spacing: var(--tracking-label); text-transform: uppercase; }
.metrics strong { display: block; margin: 10px 0 4px; font: var(--type-display); letter-spacing: -.02em; }
.metrics small { min-height: 18px; font: var(--type-label-sm); }

.table-wrap {
  width: 100%;
  overflow: auto;
  border: 1px solid var(--outline-variant);
  border-radius: var(--shape-md);
}
table { width: 100%; min-width: 760px; border-collapse: collapse; text-align: left; }
th, td { padding: 8px 12px; border-bottom: 1px solid var(--outline-variant); vertical-align: middle; }
/* Without this the selection column and the status and time cells negotiate
   for width against the prose columns and win, which pushed short values like
   a username onto three lines and made every row three lines tall. */
th:has(> input[type="checkbox"]), td:has(> input[type="checkbox"]) { width: 44px; padding-right: 0; }
td:has(> .status:only-child), td:has(> time) { white-space: nowrap; }
th {
  position: sticky;
  top: 0;
  z-index: 1;
  background: var(--surface-container);
  color: var(--on-surface-variant);
  font: var(--type-label-sm);
  letter-spacing: var(--tracking-label);
  text-transform: uppercase;
  white-space: nowrap;
}
td { max-width: 300px; font-size: 13px; }
tbody tr:last-child td { border-bottom: 0; }
tbody tr { transition: background var(--duration-short) var(--ease-standard); }
tbody tr:hover td { background: var(--surface-high); }
.secondary { color: var(--on-surface-variant); }
.nowrap { white-space: nowrap; }
.positive { color: var(--success); font-weight: 500; }
.negative, .error-cell { color: var(--error); }
.wrap-cell { min-width: 220px; white-space: normal; overflow-wrap: anywhere; }
.cell-note { display: block; margin-top: 2px; color: var(--outline); font-size: 11px; font-weight: 400; }
.identity-cell { white-space: nowrap; }
.identity-cell .cell-note { display: inline; margin: 0 0 0 6px; }
.identity-cell .cell-note::before { content: "· "; }
.truncate-code, .id-chip {
  padding: 2px 5px;
  border-radius: 4px;
  background: var(--surface-high);
}
.truncate-code {
  display: block;
  max-width: 180px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.truncate-code.copied::after { content: " · 已复制"; color: var(--success); }
.table-empty { padding: 40px 24px; color: var(--on-surface-variant); font: var(--type-body-sm); text-align: center; }
.resource-name { color: var(--on-surface); font-weight: 500; }
.row-action, .text-link { color: var(--primary); font-size: 13px; font-weight: 500; white-space: nowrap; }

.media-gallery {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
  gap: 12px;
}
.media-gallery.review-media { margin: 16px 0; }
.media-preview {
  display: grid;
  overflow: hidden;
  border-radius: 12px;
  background: var(--surface-low);
  color: var(--on-surface-variant);
}
.media-preview img {
  width: 100%;
  height: 180px;
  object-fit: contain;
  background: var(--surface-container);
}
.media-preview span { padding: 9px 12px; font-size: 12px; }
.review-downloads { display: flex; flex-wrap: wrap; gap: 8px; margin: 12px 0 16px; }
.review-downloads .outlined-button { max-width: 100%; overflow: hidden; text-overflow: ellipsis; }

.status {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  min-height: 24px;
  padding: 2px 10px;
  border: 1px solid transparent;
  border-radius: var(--shape-full);
  font: var(--type-label);
  font-size: 12px;
  white-space: nowrap;
}
.status::before {
  content: "";
  width: 6px;
  height: 6px;
  border-radius: var(--shape-full);
  background: currentColor;
}
.status.success { color: var(--success); background: color-mix(in srgb, var(--success-container) 60%, transparent); border-color: color-mix(in srgb, var(--success) 32%, transparent); }
.status.warning { color: var(--warning); background: color-mix(in srgb, var(--warning-container) 60%, transparent); border-color: color-mix(in srgb, var(--warning) 32%, transparent); }
.status.danger { color: var(--error); background: color-mix(in srgb, var(--error-container) 60%, transparent); border-color: color-mix(in srgb, var(--error) 32%, transparent); }
.status.info { color: var(--info); background: color-mix(in srgb, var(--info-container) 60%, transparent); border-color: color-mix(in srgb, var(--info) 32%, transparent); }
.status.neutral { color: var(--on-surface-variant); background: var(--surface-highest); border-color: var(--outline-variant); }

/* A grid, not a flex row. Flex sized every field to its own label, so the
   columns were ragged and the submit group was pushed onto an orphan row of
   its own whenever the fields happened to fill the line. */
.filter-bar {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(var(--field-min), 1fr));
  align-items: end;
  gap: var(--form-gap);
  margin-bottom: var(--spacing);
  padding: var(--spacing);
  border: 1px solid var(--outline-variant);
  border-radius: var(--shape-lg);
  background: var(--surface);
}
/* The search box earns two columns because it takes free text; everything
   else is a short value and gets one, which is what gives the bar its rhythm. */
.filter-bar .search-field { grid-column: span 2; }
.filter-bar.resource-filters .search-field { grid-column: span 2; }
.filter-bar.embedded { margin: 0 0 16px; padding: 0 0 16px; border: 0; border-bottom: 1px solid var(--outline-variant); border-radius: 0; }
.filter-more { grid-column: 1 / -1; }
.filter-more summary {
  width: max-content;
  min-height: 36px;
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 0 14px;
  border-radius: var(--shape-full);
  color: var(--primary);
  font: var(--type-label);
  cursor: pointer;
  list-style: none;
}
summary { list-style: none; cursor: pointer; }
summary::-webkit-details-marker, summary::marker { display: none; content: ""; }
.filter-more summary::-webkit-details-marker { display: none; }
.filter-more summary:hover { background: var(--surface-high); }
.filter-more summary .icon { --icon-size: 18px; }
.filter-more-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: 12px;
  margin-top: 12px;
  padding: 12px;
  border-radius: var(--shape-md);
  background: var(--surface-low);
}
/* The submit group is a grid cell like any other, so it lands beside the last
   field rather than alone on a row below it. */
.filter-actions { display: flex; align-items: center; gap: 8px; }
.filter-actions .filled-button { flex: 1; max-width: 160px; }

/* Bulk actions are their own surface. They used to borrow the filter bar,
   which made a destructive batch look like another way to narrow the list. */
.bulk-bar {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: end;
  gap: 12px var(--form-gap);
  margin-bottom: var(--spacing);
  padding: 16px;
  border: 1px solid var(--outline-variant);
  border-radius: var(--shape-lg);
  background: var(--surface-container);
}
.bulk-bar:has([data-bulk-item]:checked), .bulk-bar.has-selection { border-color: var(--primary); }
.bulk-selection { grid-column: 1 / -1; display: flex; align-items: baseline; flex-wrap: wrap; gap: 10px; }
.bulk-selection [data-bulk-count] { font: var(--type-title); color: var(--on-surface); }
.bulk-hint { color: var(--on-surface-variant); font: var(--type-label-sm); }
.bulk-controls { grid-column: 1 / -1; display: flex; align-items: end; flex-wrap: wrap; gap: var(--form-gap); }
.bulk-controls label { min-width: 170px; }

.bulk-note { min-width: 0; }
.bulk-note textarea { min-height: 56px; }
.bulk-decisions { display: flex; flex-wrap: wrap; gap: 8px; }

/* Priority and SLA are the two things that decide what a reviewer opens next,
   so they get a shape of their own rather than another neutral status pill. */
.priority-chip {
  display: inline-flex;
  align-items: center;
  padding: 2px 10px;
  border: 1px solid var(--outline-variant);
  border-radius: var(--shape-sm);
  color: var(--on-surface-variant);
  font: var(--type-label-sm);
  white-space: nowrap;
}
.priority-chip.priority-1 { border-color: color-mix(in srgb, var(--info) 45%, transparent); color: var(--info); }
.priority-chip.priority-2 { border-color: color-mix(in srgb, var(--warning) 45%, transparent); color: var(--warning); }
.priority-chip.priority-3 { border-color: color-mix(in srgb, var(--error) 55%, transparent); background: color-mix(in srgb, var(--error-container) 45%, transparent); color: var(--error); font-weight: 700; }
.sla-late { color: var(--error); font-weight: 500; }
.row-overdue td:first-child { box-shadow: inset 3px 0 0 var(--error); }
.signal-cell { display: table-cell; }
.signal-cell .status { margin-right: 6px; }
/* ---------------------------------------------------------------------------
   Forms

   Every console form is the same two things: a caption above a control, and a
   row of buttons that acts on them. Both used to be redefined per page, which
   is why the same field was 12px here and 13px there, and why focus nudged the
   layout. They are defined once below and every page inherits them.
   --------------------------------------------------------------------------- */

/* The field primitive. 161 of the 161 captioned controls in the templates are
   written as <label><span>Caption</span><control></label>, so the primitive
   attaches to that shape directly and no template has to opt in. */
label:has(> span) {
  display: grid;
  align-content: start;
  gap: var(--field-gap);
  min-width: 0;
}
label > span:first-child {
  color: var(--on-surface-variant);
  font: var(--type-label-sm);
  letter-spacing: .01em;
}
/* A required control says so next to its caption instead of leaving the
   operator to discover it by submitting. */
label:has(> :is(input, select, textarea)[required]) > span:first-child::after {
  content: " *";
  color: var(--error);
}

input, select, textarea {
  width: 100%;
  min-width: 0;
  border: 1px solid var(--outline);
  border-radius: var(--shape-xs);
  outline: 0;
  background: var(--surface-lowest);
  color: var(--on-surface);
  padding: 0 var(--control-pad);
  font: var(--type-body);
  font-family: inherit;
  transition:
    border-color var(--duration-short) var(--ease-standard),
    box-shadow var(--duration-short) var(--ease-standard),
    background-color var(--duration-short) var(--ease-standard);
}
input, select { height: var(--control-height); }
textarea { min-height: 84px; padding: 10px var(--control-pad); resize: vertical; line-height: 1.5; }
input::placeholder, textarea::placeholder { color: var(--on-surface-variant); opacity: .7; }
input:hover, select:hover, textarea:hover { border-color: var(--on-surface-variant); }

/* The focus ring is drawn outside the box. The old rule thickened the border
   and shaved a pixel off the padding to compensate, so every field twitched
   the moment it was focused. */
input:focus-visible, select:focus-visible, textarea:focus-visible,
input:focus, select:focus, textarea:focus {
  border-color: var(--primary);
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--primary) 45%, transparent);
}
:is(input, select, textarea)[aria-invalid="true"] {
  border-color: var(--error);
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--error) 30%, transparent);
}
:is(input, select, textarea):disabled {
  border-color: var(--outline-variant);
  background: var(--surface-high);
  color: var(--on-surface-variant);
  cursor: not-allowed;
}

/* Native select arrows differ per platform and sit at a different height than
   the text in our other controls, so the control draws its own. */
select {
  appearance: none;
  padding-right: 34px;
  background-image: linear-gradient(45deg, transparent 50%, currentColor 50%), linear-gradient(135deg, currentColor 50%, transparent 50%);
  background-position: right 16px center, right 11px center;
  background-size: 5px 5px, 5px 5px;
  background-repeat: no-repeat;
  cursor: pointer;
}

input[type="checkbox"], input[type="radio"] {
  width: 18px;
  height: 18px;
  min-height: 0;
  padding: 0;
  accent-color: var(--primary);
  cursor: pointer;
}
input[type="checkbox"]:focus-visible, input[type="radio"]:focus-visible {
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--primary) 55%, transparent);
}
input[type="date"], input[type="datetime-local"] { padding-right: 10px; }
input[type="file"] { height: auto; padding: 8px var(--control-pad); }
input[type="file"]::file-selector-button {
  margin-right: 12px;
  padding: 6px 14px;
  border: 0;
  border-radius: var(--shape-full);
  background: var(--secondary-container);
  color: var(--on-secondary-container);
  font: var(--type-label);
  cursor: pointer;
}
.filter-reset { align-self: center; padding: 10px 4px; }

.filled-button, .outlined-button, .full-button {
  position: relative;
  isolation: isolate;
  overflow: hidden;
  min-height: 40px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 8px 22px;
  border: 1px solid transparent;
  border-radius: var(--shape-full);
  font: var(--type-label);
  font-size: 14px;
  cursor: pointer;
  white-space: nowrap;
  transition: border-radius var(--duration-medium) var(--ease-emphasized), box-shadow var(--duration-short) var(--ease-standard);
}
/* One state layer for every interactive surface, at the spec opacities. */
.filled-button::before, .outlined-button::before, .full-button::before,
.icon-button::before, .nav-link::before {
  content: "";
  position: absolute;
  inset: 0;
  z-index: -1;
  background: currentColor;
  opacity: 0;
  transition: opacity var(--duration-short) var(--ease-standard);
}
.filled-button:hover::before, .outlined-button:hover::before, .full-button:hover::before,
.icon-button:hover::before, .nav-link:hover::before { opacity: var(--state-hover); }
.filled-button:active::before, .outlined-button:active::before, .full-button:active::before,
.icon-button:active::before, .nav-link:active::before { opacity: var(--state-pressed); }
/* Expressive: the corner relaxes on press rather than the button just dimming. */
.filled-button:active, .outlined-button:active, .full-button:active { border-radius: var(--shape-sm); }
.filled-button { background: var(--primary); color: var(--on-primary); box-shadow: var(--elevation-1); }
.filled-button:hover { box-shadow: var(--elevation-2); }
.outlined-button { border-color: var(--outline); background: transparent; color: var(--primary); }
.outlined-button.danger { color: var(--error); }
.submitting, [aria-disabled="true"] { pointer-events: none; opacity: .65; }
.submitting::after {
  content: "";
  width: 14px;
  height: 14px;
  border: 2px solid currentColor;
  border-right-color: transparent;
  border-radius: 50%;
  animation: spin .7s linear infinite;
}
@keyframes spin { to { transform: rotate(360deg); } }
.actions { display: flex; justify-content: flex-end; flex-wrap: wrap; gap: 8px; }

.notice { margin-bottom: var(--spacing); padding: 12px 16px; border-radius: var(--shape-md); font: var(--type-body-sm); }
.notice.success { color: var(--success); background: var(--success-container); }
.notice.danger { color: var(--on-error-container); background: var(--error-container); }
.form-error-summary {
  grid-column: 1 / -1;
  width: 100%;
  margin-bottom: 12px;
  padding: 10px 12px;
  border-left: 4px solid var(--error);
  border-radius: var(--shape-xs);
  background: var(--error-container);
  color: var(--on-error-container);
  font-weight: 500;
}
.form-error-summary[hidden] { display: none; }
.form-failure { max-width: 720px; display: grid; gap: 16px; }
.form-failure .notice { margin: 0; }
[aria-invalid="true"] { border-color: var(--error); }
.toast-notice {
  position: fixed;
  z-index: 10000;
  top: 16px;
  left: 50%;
  margin: 0;
  box-shadow: 0 4px 12px rgb(0 0 0 / .15);
  transform: translateX(-50%);
  animation: toast-in .22s cubic-bezier(.2, 0, 0, 1);
}
.toast-notice.leaving { opacity: 0; transform: translate(-50%, -12px); transition: .2s ease; }
@keyframes toast-in {
  from { opacity: 0; transform: translate(-50%, -12px); }
  to { opacity: 1; transform: translate(-50%, 0); }
}
.muted { color: var(--on-surface-variant); }
.diagnostic { margin: 0; font-family: var(--font-mono); overflow-wrap: anywhere; }

.review-summary { display: grid; grid-template-columns: minmax(0, 1fr) minmax(340px, .8fr); gap: 20px; }
.review-summary p { margin: 6px 0 0; }
.review-meta { margin: 0; display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px 16px; }
.review-meta div { min-width: 0; }
.review-meta dt, .settings dt, .target-box dt { color: var(--outline); font-size: 11px; font-weight: 500; }
.review-meta dd, .settings dd, .target-box dd { margin: 2px 0 0; overflow-wrap: anywhere; }
.review-findings {
  margin-top: 14px;
  padding: 12px 14px;
  border-radius: 8px;
  background: var(--warning-container);
  color: var(--warning);
}
.review-findings ul { margin: 6px 0 0; padding-left: 20px; }
.review-findings .resolved { opacity: .65; text-decoration: line-through; }
.snapshot { margin-top: 14px; border-block: 1px solid var(--outline-variant); }
.snapshot summary { padding: 11px 0; color: var(--primary); font-weight: 500; cursor: pointer; }
.snapshot pre {
  max-height: 420px;
  margin: 0 0 12px;
  padding: 12px;
  overflow: auto;
  border-radius: 8px;
  background: var(--surface-low);
  color: var(--on-surface-variant);
  font-size: 11px;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.decision-form { margin-top: 14px; display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
.decision-form .actions { grid-column: 1 / -1; }
.review-decision-form { display: grid; grid-template-columns: 1fr; gap: 16px; }
.review-controls { display: grid; grid-template-columns: minmax(0, 2fr) minmax(240px, 1fr); gap: 16px; align-items: start; }
.review-tags { min-width: 0; margin: 0; padding: 14px; border: 1px solid var(--outline-variant); border-radius: 8px; }
.review-tags legend { padding: 0 6px; color: var(--on-surface-variant); font-size: 12px; font-weight: 500; }
.chip-row { display: flex; flex-wrap: wrap; gap: 8px; }
.chip-row label { width: auto; display: inline-flex; align-items: center; gap: 8px; padding: 8px 12px; border: 1px solid var(--outline-variant); border-radius: 18px; color: var(--on-surface); font-size: 13px; cursor: pointer; }
.chip-row label:has(input:checked) { border-color: var(--primary); background: var(--secondary-container); color: var(--on-secondary-container); }
.review-grade { align-self: start; }
.review-note textarea { min-height: 120px; }
.review-decision-form .actions { grid-column: auto; }
.review-workbench { display: grid; grid-template-columns: minmax(0, 1fr) 360px; gap: 20px; align-items: start; }
.review-workbench-main { display: grid; gap: 20px; min-width: 0; }
.review-submission-summary { display: grid; gap: 12px; }
.review-submission-summary h2 { margin: 2px 0 0; font-size: 24px; }
.review-submission-summary .section-header { align-items: start; }
.review-summary-text { max-width: 78ch; margin: 0; white-space: pre-wrap; line-height: 1.7; color: var(--on-surface-variant); }
.review-decision-rail { min-width: 0; }
.sticky-review-decision {
  position: sticky;
  top: 82px;
  max-height: calc(100vh - 98px);
  overflow: auto;
  display: grid;
  gap: 16px;
  align-content: start;
}
.sticky-review-decision form { display: grid; gap: 14px; }
.sticky-review-decision hr { width: 100%; border: 0; border-top: 1px solid var(--outline-variant); }
.decision-actions {
  position: sticky;
  bottom: -1px;
  z-index: 2;
  display: grid;
  gap: 8px;
  margin: 8px -20px -20px;
  padding: 12px 20px 20px;
  background: linear-gradient(transparent, var(--surface) 16px);
}
.publication-plan-cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(230px, 1fr)); gap: 12px; }
.publication-plan-card { border: 1px solid var(--outline-variant); border-radius: 12px; padding: 14px; background: var(--surface-container-low); min-width: 0; }
.publication-plan-card h3 { margin: 0; font-size: 16px; }
.publication-target-list { display: grid; gap: 8px; }
.publication-target-list > div { display: grid; grid-template-columns: auto auto minmax(0, 1fr); gap: 8px; align-items: baseline; padding: 8px 10px; border-radius: 8px; background: var(--surface-container); }
.publication-target-list code { overflow-wrap: anywhere; }
.artifact-review-list { display: grid; gap: 14px; }
.artifact-review-card { border: 1px solid var(--outline-variant); border-radius: 12px; padding: 16px; display: grid; gap: 14px; min-width: 0; }
.artifact-review-head { display: flex; justify-content: space-between; align-items: start; gap: 16px; }
.artifact-review-head > div { min-width: 0; display: grid; gap: 4px; }
.artifact-review-head code { overflow-wrap: anywhere; }
.artifact-device-summary { display: grid; gap: 8px; }
.artifact-device-summary .chip-row { gap: 6px; }
.artifact-device-summary small { color: inherit; opacity: .75; }
.artifact-device-editor { border-top: 1px solid var(--outline-variant); padding-top: 12px; }
.artifact-device-editor > summary { display: inline-flex; cursor: pointer; list-style: none; }
.artifact-device-editor > form { display: grid; gap: 12px; margin-top: 14px; }
.inline-editor { position: relative; }
/* Now that the browser marker is gone, the trigger draws its own caret so it
   still reads as something that opens rather than as plain text. */
.inline-editor > summary { display: inline-flex; align-items: center; gap: 5px; }
.inline-editor > summary::after {
  content: "";
  width: 5px;
  height: 5px;
  border-right: 1.5px solid currentColor;
  border-bottom: 1.5px solid currentColor;
  transform: rotate(45deg) translate(-1px, -1px);
  transition: transform var(--duration-short) var(--ease-standard);
}
.inline-editor[open] > summary::after { transform: rotate(-135deg) translate(-1px, -1px); }
.inline-editor > form { position: absolute; z-index: 5; right: 0; top: calc(100% + 8px); width: min(420px, 90vw); padding: 16px; border: 1px solid var(--outline-variant); border-radius: 12px; background: var(--surface-container-high); box-shadow: var(--shadow-2); display: grid; gap: 12px; }
.review-inline-editor > summary { display: flex; align-items: center; gap: 8px; cursor: pointer; list-style: none; }
.review-inline-editor > summary span:last-child { margin-left: auto; color: var(--outline); font-size: 12px; font-weight: 400; }
.review-inline-editor > form { margin-top: 20px; }
.comparison-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; margin-top: 16px; }
.field-diff-list { display: grid; gap: 12px; margin-top: 16px; }
.field-diff { padding: 12px 14px; border: 1px solid var(--outline-variant); border-radius: 12px; background: var(--surface-container-low); }
.field-diff h3 { margin: 0 0 8px; font-size: 13px; }
.field-diff-pair { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.field-diff-before, .field-diff-after { min-width: 0; }
.field-diff-before span, .field-diff-after span { color: var(--on-surface-variant); font: var(--type-label-sm); }
.field-diff pre { margin: 6px 0 0; white-space: pre-wrap; overflow-wrap: anywhere; font: var(--type-body-sm); }
.field-diff-before pre { color: var(--error); }
.field-diff-after pre { color: var(--success); }
.field-diff.added { border-color: color-mix(in srgb, var(--success) 40%, transparent); }
.field-diff.removed { border-color: color-mix(in srgb, var(--error) 40%, transparent); }
.checklist-grid { display: grid; grid-template-columns: 1fr; gap: 8px; margin: 0; padding: 12px; border: 1px solid var(--outline-variant); border-radius: 12px; }
.binding-row { align-items: start; }
.binding-details { display: grid; gap: 8px; }
.binding-details > div { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.binding-details span { min-width: 0; display: grid; gap: 2px; }
.binding-actions { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 8px; }
.inline-role-form, .row-form { display: inline-flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.inline-role-form select, .row-form input { width: auto; min-width: 132px; height: 36px; }

.header-actions, .composer-actions, .composer-footer, .blog-list-actions, .blog-editor-actions > div { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; }
.home-composer { display: grid; gap: 16px; }
.composer-section { margin: 0; }
.composer-list { display: grid; gap: 10px; }
.composer-list.compact { margin-top: 12px; }
.composer-item { display: grid; grid-template-columns: 132px minmax(0, 1fr) auto; gap: 14px; align-items: center; padding: 12px; border: 1px solid var(--outline-variant); border-radius: 12px; background: var(--surface-low); }
.composer-list.compact .composer-item { grid-template-columns: auto minmax(0, 1fr) auto; }
.composer-cover { width: 132px; aspect-ratio: 16 / 9; display: grid; place-items: center; overflow: hidden; border-radius: 9px; background: var(--surface-highest); color: var(--outline); }
.composer-cover.large { width: 180px; }
.composer-cover img { width: 100%; height: 100%; object-fit: cover; }
.composer-copy { min-width: 0; }
.composer-copy h3 { margin: 0; font-size: 16px; }
.composer-copy p { margin: 5px 0; color: var(--on-surface-variant); }
.composer-kind { color: var(--primary); }
.composer-actions form, .composer-footer form, .blog-list-actions form { display: flex; margin: 0; }
.composer-editor, .composer-add { position: relative; }
.composer-editor > summary, .composer-add > summary { list-style: none; cursor: pointer; }
.composer-editor > summary::-webkit-details-marker, .composer-add > summary::-webkit-details-marker { display: none; }
.composer-editor[open] > .composer-form, .composer-add[open] > .composer-form { position: absolute; z-index: 20; top: calc(100% + 8px); inset-inline-end: 0; width: min(520px, calc(100vw - 32px)); max-height: min(640px, calc(100vh - 96px)); overflow: auto; padding: 18px; border: 1px solid var(--outline-variant); border-radius: 12px; background: var(--surface-container); box-shadow: 0 10px 32px rgb(0 0 0 / .18); }
.composer-form { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }

.composer-form .actions { grid-column: 1 / -1; justify-content: flex-end; }
.toggle-label { display: flex !important; align-items: center; gap: 9px !important; }
.section-kicker { color: var(--primary); font-size: 11px; font-weight: 600; letter-spacing: .08em; }
.builtin-section { display: flex; align-items: center; justify-content: space-between; gap: 16px; }
.builtin-section h2 { margin-top: 4px; }
.builtin-section p { margin: 4px 0 0; color: var(--on-surface-variant); }
.composer-footer { justify-content: flex-end; margin-top: 14px; padding-top: 14px; border-top: 1px solid var(--outline-variant); }
.composer-create { margin: 0; }
.composer-create > summary { display: flex; align-items: center; justify-content: center; gap: 8px; color: var(--primary); font-weight: 500; cursor: pointer; }
.composer-create[open] > summary { justify-content: flex-start; margin-bottom: 16px; }
.composer-empty, .panel-empty { padding: 32px 24px; border: 1px dashed var(--outline-variant); border-radius: var(--shape-md); color: var(--on-surface-variant); font: var(--type-body-sm); text-align: center; }
.panel-empty { margin: 0; }

.blog-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 14px; }
.blog-list { display: grid; gap: 12px; }
.blog-list-card { display: grid; grid-template-columns: 180px minmax(0, 1fr) auto; gap: 16px; align-items: center; overflow: hidden; padding: 12px; border: 1px solid var(--outline-variant); border-radius: 12px; background: var(--surface); }
.blog-list-card > img, .blog-cover-placeholder { width: 180px; aspect-ratio: 16 / 9; object-fit: cover; border-radius: 9px; background: var(--surface-highest); }
.blog-cover-placeholder { display: grid; place-items: center; color: var(--outline); }
.blog-list-copy { min-width: 0; }
.blog-list-copy h2 { margin: 0; font-size: 17px; }
.blog-list-copy > p { margin: 7px 0; color: var(--on-surface-variant); }
.blog-meta { display: flex; flex-wrap: wrap; gap: 12px; color: var(--outline); font-size: 11px; }
.admin-dialog { width: min(560px, calc(100% - 32px)); padding: 22px; border: 0; border-radius: 16px; background: var(--surface-container); color: var(--on-surface); box-shadow: 0 12px 40px rgb(0 0 0 / .24); }
.admin-dialog::backdrop { background: var(--scrim); }
.admin-dialog .composer-form { grid-template-columns: 1fr; }
.admin-dialog .section-header, .admin-dialog .actions { grid-column: 1; }
.blog-editor { display: grid; gap: 16px; }
.blog-fields { display: grid; gap: 14px; }
.blog-field-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
.cover-field { display: flex; align-items: center; gap: 16px; padding-top: 14px; border-top: 1px solid var(--outline-variant); }
.cover-field p { margin: 4px 0 10px; }
.blog-writing { padding: 0; overflow: hidden; }
.writing-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 12px 16px; border-bottom: 1px solid var(--outline-variant); }
.writing-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); min-height: 560px; }
.writing-grid > label { padding: 14px; border-right: 1px solid var(--outline-variant); }
.writing-grid textarea { min-height: 500px; resize: vertical; font-family: "JetBrains Mono", ui-monospace, monospace; line-height: 1.6; }
.writing-preview { min-width: 0; padding: 14px 18px; }
.writing-preview > span { color: var(--outline); font-size: 12px; font-weight: 500; }
.writing-preview article { margin-top: 14px; white-space: pre-wrap; overflow-wrap: anywhere; line-height: 1.7; }
.blog-editor-actions { position: sticky; z-index: 10; bottom: 0; display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 12px 16px; border: 1px solid var(--outline-variant); border-radius: 12px; background: var(--surface-container); box-shadow: 0 -4px 18px rgb(0 0 0 / .08); }

.ticket-list { display: grid; gap: 12px; }
.ticket-card { margin: 0; }
.ticket-card > header { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; }
.ticket-card h2 { font-size: 16px; }
.ticket-meta { margin: 5px 0 0; font-size: 12px; }
.ticket-message {
  margin-top: 12px;
  padding: 12px;
  border-radius: 8px;
  background: var(--surface-low);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.ticket-excerpt {
  display: -webkit-box;
  margin: 12px 0 8px;
  overflow: hidden;
  color: var(--on-surface-variant);
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 3;
}
.ticket-target { display: flex; align-items: center; gap: 6px; color: var(--outline); font-size: 12px; }
.ticket-target .row-action { margin-left: auto; }
.target-box {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
  margin: 10px 0 0;
  padding: 12px;
  border-radius: 8px;
  background: var(--surface-container);
}
.reply-thread { margin-top: 12px; }
.reply-thread h3 { color: var(--on-surface-variant); }
.reply-thread article { margin-top: 8px; padding: 10px 12px; border-left: 3px solid var(--primary); background: var(--surface-low); }
.reply-thread article > div { display: flex; justify-content: space-between; gap: 10px; }
.reply-thread time { color: var(--outline); font-size: 11px; }
.reply-thread p { margin: 5px 0 0; white-space: pre-wrap; }
.reply-form { margin-top: 12px; }
.reply-form .actions { margin-top: 10px; }
.report-form { display: grid; grid-template-columns: minmax(180px, .45fr) minmax(0, 1.55fr); gap: 12px; }


.settings { margin: 0; display: grid; grid-template-columns: 140px minmax(0, 1fr); gap: 10px 16px; }
.settings.compact { grid-template-columns: 100px minmax(0, 1fr); }
.stack-list { display: grid; gap: 8px; }
.stack-row {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 10px;
  padding: 10px 12px;
  border-radius: 8px;
  background: var(--surface-low);
}
.stack-row .cell-note { color: var(--primary); }
.row-error { width: 100%; margin: 6px 0 0; color: var(--error); }
.tag-stack { display: flex; align-items: center; flex-wrap: wrap; gap: 5px; }
.management-actions { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; }
.management-actions form { margin: 0; }
.subtabs { display: flex; gap: 4px; margin: -4px 0 16px; border-bottom: 1px solid var(--outline-variant); }
.subtabs a {
  min-height: 40px;
  display: inline-flex;
  align-items: center;
  padding: 0 14px;
  border-radius: 20px 20px 0 0;
  color: var(--on-surface-variant);
  font-size: 13px;
  font-weight: 500;
}
.subtabs a:hover { background: var(--surface-high); }
.subtabs a.active { color: var(--primary); border-bottom: 2px solid var(--primary); }
.pagination {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-top: 16px;
  color: var(--on-surface-variant);
  font-size: 13px;
}
.pagination > div { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; }
.pagination-summary { min-height: 40px; }
.page-size,
.page-number {
  min-width: 32px;
  height: 32px;
  display: inline-grid;
  place-items: center;
  padding: 0 8px;
  border-radius: 16px;
  color: var(--on-surface-variant);
  font-size: 12px;
  font-weight: 500;
}
.page-size:hover,
.page-number:hover { background: var(--surface-high); color: var(--on-surface); }
.page-size.active,
.page-number.active { background: var(--secondary-container); color: var(--on-secondary-container); }
.pagination-edge { width: 36px; height: 36px; }
.pagination .disabled { pointer-events: none; opacity: .38; }

.revision-editor { display: grid; gap: var(--spacing); padding-bottom: 72px; }
.revision-editor > .panel { margin: 0; }
.editor-field-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 14px; }
.editor-field-grid .span-2 { grid-column: 1 / -1; }
.choice-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 8px; }
.choice-card { min-height: 52px; display: flex; align-items: center; gap: 10px; padding: 10px 12px; border: 1px solid var(--outline-variant); border-radius: 12px; background: var(--surface-low); cursor: pointer; }
/* Selection uses the secondary container, matching .chip-row. The primary
   container is the loudest surface in the palette and a reviewer ticking four
   tags at once ended up with a wall of solid purple. */
.choice-card:has(input:checked) { border-color: var(--primary); background: var(--secondary-container); color: var(--on-secondary-container); }
.choice-card span { display: grid; }
.choice-card small { color: var(--on-surface-variant); }
.link-editor { display: grid; gap: 8px; }
.link-row { display: grid; grid-template-columns: minmax(160px, .55fr) minmax(240px, 1.45fr) 40px; gap: 8px; align-items: center; }
.json-textarea { min-height: 180px; font-family: "Roboto Mono", ui-monospace, monospace; line-height: 1.5; }
.compact-diagnostic { max-height: 160px; margin: 0; }
.editor-assets { display: flex; flex-wrap: wrap; gap: 8px; }
.editor-assets span { padding: 8px 12px; border-radius: 16px; background: var(--surface-high); color: var(--on-surface-variant); }
.sticky-actions { position: sticky; z-index: 100; bottom: 0; min-height: 64px; display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 10px 16px; border: 1px solid var(--outline-variant); border-radius: 16px; background: color-mix(in srgb, var(--surface) 94%, transparent); box-shadow: 0 -6px 24px rgb(0 0 0 / .08); backdrop-filter: blur(12px); }
.sticky-actions > div { display: flex; gap: 8px; }

.empty-state {
  min-height: 220px;
  display: grid;
  place-items: center;
  align-content: center;
  padding: 40px 32px;
  border: 1px solid var(--outline-variant);
  border-radius: var(--shape-lg);
  background: var(--surface);
  text-align: center;
}
/* The compact variant sits inside a panel that already has a border. */
.empty-state.compact {
  min-height: 0;
  padding: 28px 24px;
  border-style: dashed;
  background: transparent;
}
.empty-state.compact .icon { --icon-size: 28px; margin-bottom: 8px; color: var(--outline); }
.empty-mark {
  width: 52px;
  height: 52px;
  display: grid;
  place-items: center;
  margin-bottom: 14px;
  border-radius: var(--shape-lg);
  background: var(--primary-container);
  color: var(--on-primary-container);
  font-size: 20px;
  font-weight: 500;
}
.empty-state h2 { font: var(--type-title-lg); }
.empty-state p { margin: 6px 0 0; }

.standalone-page {
  width: 100%;
  height: 100vh;
  height: 100dvh;
  display: grid;
  place-items: center;
  padding: 24px;
  overflow-y: auto;
  background: var(--surface-low);
}
.standalone-card {
  width: min(408px, 100%);
  padding: 32px 24px;
  border: 0;
  border-radius: 12px;
  background: var(--surface);
  box-shadow: 0 4px 24px rgb(0 0 0 / .05);
  text-align: center;
}
.standalone-icon {
  width: 56px;
  height: 56px;
  display: grid;
  place-items: center;
  margin: 0 auto 20px;
  border-radius: 16px;
  background: var(--primary-container);
  color: var(--on-primary-container);
}
.standalone-icon .icon { --icon-size: 28px; }
.standalone-card.danger .standalone-icon { background: var(--error-container); color: var(--on-error-container); }
.standalone-card.info .standalone-icon { background: var(--secondary-container); color: var(--on-secondary-container); }
.standalone-card h1 { margin: 0 0 8px; font-size: 20px; font-weight: 500; }
.standalone-card > p { margin: 0; color: var(--outline); font-size: 13px; }
.standalone-actions { margin-top: 20px; }
.full-button { width: 100%; }
.transition-retry {
  display: inline-block;
  margin-top: 12px;
  color: var(--on-surface-variant);
  font-size: 13px;
}
.transition-retry span {
  color: var(--primary);
  text-decoration: underline;
  text-underline-offset: 3px;
}
.server-card { width: min(420px, 100%); }

.confirm-dialog {
  width: min(400px, calc(100% - 32px));
  padding: 24px;
  border: 0;
  border-radius: 28px;
  background: var(--surface-container);
  color: var(--on-surface);
  box-shadow: 0 8px 32px rgb(0 0 0 / .2);
}
.confirm-dialog::backdrop { background: var(--scrim); }
.confirm-dialog form { margin: 0; }
.confirm-dialog h2 { margin: 16px 0 8px; font-size: 24px; font-weight: 400; }
.confirm-dialog p { margin: 0; white-space: pre-wrap; }
.dialog-icon { color: var(--error); }
.dialog-icon .icon { --icon-size: 28px; }
.dialog-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 24px; }

@media (max-width: 1100px) {
  .dashboard-grid { grid-template-columns: 1fr; }
  .dashboard-grid .span-2 { grid-column: auto; }
  .metrics.system-metrics { grid-template-columns: repeat(2, minmax(150px, 1fr)); }
}

@media (max-width: 900px) {
  body.drawer-collapsed .nav { margin-left: 0; }
  .nav {
    position: fixed;
    top: var(--header-height);
    bottom: 0;
    left: 0;
    transform: translateX(-100%);
    border-right: 0;
  }
  .nav.open { transform: translateX(0); box-shadow: 2px 0 12px rgb(0 0 0 / .12); }
  .drawer-overlay {
    position: fixed;
    z-index: 500;
    inset: var(--header-height) 0 0;
    background: var(--scrim);
  }
  .drawer-overlay.open { display: block; }
  .review-summary { grid-template-columns: 1fr; }
}

@media (max-width: 700px) {
  .header-brand { font-size: 16px; }
  .admin-main { padding: 12px; }
  .page-header { margin-bottom: 12px; flex-wrap: wrap; }
  .content-grid, .settings-grid, .detail-grid { grid-template-columns: 1fr; gap: 12px; }
  .span-2 { grid-column: auto; }
  .metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; }
  .metrics.system-metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .filter-bar { padding: 12px; align-items: stretch; flex-direction: column; }
  .filter-bar label, .filter-bar .search-field { width: 100%; min-width: 0; }
  .filter-bar.resource-filters { display: flex; }
  .filter-bar.resource-filters .search-field { grid-column: auto; }
  .filter-reset { align-self: center; }
  .decision-form, .report-form { grid-template-columns: 1fr; }
  .decision-form .actions { grid-column: auto; }
  .review-meta, .target-box { grid-template-columns: 1fr; }
  .review-controls { grid-template-columns: 1fr; }
  .review-workbench { grid-template-columns: 1fr; }
  .sticky-review-decision { position: static; }
  .comparison-grid, .field-diff-pair { grid-template-columns: 1fr; }
  .artifact-review-head { flex-direction: column; }
  .settings { grid-template-columns: 1fr; gap: 3px; }
  .settings dd { margin-bottom: 7px; }
  .editor-field-grid { grid-template-columns: 1fr; }
  .editor-field-grid .span-2 { grid-column: auto; }
  .link-row { grid-template-columns: minmax(0, 1fr) 40px; }
  .link-row input[type="url"] { grid-column: 1 / -1; grid-row: 2; }
  .link-row .icon-button { grid-column: 2; grid-row: 1; }
  .sticky-actions { align-items: stretch; flex-direction: column; }
  .sticky-actions > div { justify-content: flex-end; }
  .ticket-card > header { flex-direction: column; }
  .pagination { align-items: flex-start; flex-direction: column; }
  .standalone-page { padding: 16px; }
  .header-actions { width: 100%; }
  .composer-item, .composer-list.compact .composer-item, .blog-list-card { grid-template-columns: 1fr; }
  .composer-cover, .blog-list-card > img, .blog-cover-placeholder { width: 100%; }
  .composer-actions, .blog-list-actions { justify-content: flex-end; }
  .composer-form, .blog-field-grid, .writing-grid { grid-template-columns: 1fr; }
  .writing-grid > label { border-right: 0; border-bottom: 1px solid var(--outline-variant); }
  .blog-editor-actions { align-items: stretch; flex-direction: column; }
  .table-wrap { overflow: visible; border: 0; background: transparent; }
  .table-wrap table,
  .table-wrap tbody,
  .table-wrap tr,
  .table-wrap td { display: block; width: 100%; }
  .table-wrap table { min-width: 0; }
  .table-wrap thead { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0, 0, 0, 0); }
  .table-wrap tbody { display: grid; gap: 10px; }
  .table-wrap tbody tr {
    overflow: hidden;
    border: 1px solid var(--outline-variant);
    border-radius: 12px;
    background: var(--surface);
  }
  .table-wrap tbody td {
    min-width: 0;
    padding: 9px 12px;
    border-bottom: 1px solid var(--outline-variant);
    white-space: normal;
    overflow-wrap: anywhere;
    text-align: left;
  }
  .table-wrap tbody td:last-child { border-bottom: 0; }
  .table-wrap tbody td[data-label]::before {
    content: attr(data-label);
    display: block;
    margin-bottom: 3px;
    color: var(--outline);
    font-size: 11px;
    font-weight: 700;
    letter-spacing: .03em;
  }
  .table-wrap tbody td.table-empty { padding: 28px 16px; text-align: center; }
  .table-wrap tbody td.table-empty::before { display: none; }
}

@media (prefers-reduced-motion: reduce) {
  :root { --duration-short: 0ms; --duration-medium: 0ms; --duration-long: 0ms; }
  *, *::before, *::after {
    scroll-behavior: auto !important;
    animation-duration: .01ms !important;
    animation-iteration-count: 1 !important;
    transition-duration: .01ms !important;
  }
}

@media (forced-colors: active) {
  .filled-button, .outlined-button, .icon-button, input, select, textarea, .panel, .table-wrap tbody tr {
    border: 1px solid ButtonText;
  }
  .status, .count-badge { border: 1px solid CanvasText; }
}
`
