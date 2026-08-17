package web

const CSS = `
:root {
  color-scheme: light;
  --primary: #6750a4;
  --on-primary: #ffffff;
  --primary-container: #eaddff;
  --on-primary-container: #21005d;
  --secondary: #625b71;
  --secondary-container: #e8def8;
  --on-secondary-container: #1d192b;
  --surface: #fef7ff;
  --on-surface: #1d1b20;
  --on-surface-variant: #49454f;
  --surface-lowest: #ffffff;
  --surface-low: #f7f2fa;
  --surface-container: #f3edf7;
  --surface-high: #ece6f0;
  --surface-highest: #e6e0e9;
  --surface-container-low: var(--surface-low);
  --surface-container-high: var(--surface-high);
  --shadow-2: 0 10px 32px rgb(0 0 0 / .18);
  --outline: #79747e;
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
  --header-height: 56px;
  --drawer-width: 260px;
  --spacing: 16px;
  font-family: Roboto, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
}

:root.dark-theme {
  color-scheme: dark;
  --primary: #d0bcff;
  --on-primary: #381e72;
  --primary-container: #4f378b;
  --on-primary-container: #eaddff;
  --secondary: #ccc2dc;
  --secondary-container: #4a4458;
  --on-secondary-container: #e8def8;
  --surface: #141218;
  --on-surface: #e6e1e5;
  --on-surface-variant: #cac4d0;
  --surface-lowest: #0f0d13;
  --surface-low: #1d1b20;
  --surface-container: #211f26;
  --surface-high: #2b2930;
  --surface-highest: #36343b;
  --surface-container-low: var(--surface-low);
  --surface-container-high: var(--surface-high);
  --shadow-2: 0 10px 32px rgb(0 0 0 / .35);
  --outline: #938f99;
  --outline-variant: #49454f;
  --error: #f2b8b5;
  --on-error: #601410;
  --error-container: #8c1d18;
  --on-error-container: #f9dedc;
  --success: #57cc65;
  --success-container: #00531a;
  --warning: #f2c96d;
  --warning-container: #594400;
  --info: #bdc7eb;
  --info-container: #3d4663;
}

@media (prefers-color-scheme: dark) {
  :root:not(.light-theme):not(.dark-theme) {
    color-scheme: dark;
    --primary: #d0bcff;
    --on-primary: #381e72;
    --primary-container: #4f378b;
    --on-primary-container: #eaddff;
    --secondary: #ccc2dc;
    --secondary-container: #4a4458;
    --on-secondary-container: #e8def8;
    --surface: #141218;
    --on-surface: #e6e1e5;
    --on-surface-variant: #cac4d0;
    --surface-lowest: #0f0d13;
    --surface-low: #1d1b20;
    --surface-container: #211f26;
    --surface-high: #2b2930;
    --surface-highest: #36343b;
    --outline: #938f99;
    --outline-variant: #49454f;
    --error: #f2b8b5;
    --on-error: #601410;
    --error-container: #8c1d18;
    --on-error-container: #f9dedc;
    --success: #57cc65;
    --success-container: #00531a;
    --warning: #f2c96d;
    --warning-container: #594400;
    --info: #bdc7eb;
    --info-container: #3d4663;
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
  font: 400 14px/1.5 Roboto, system-ui, sans-serif;
  -webkit-font-smoothing: antialiased;
}
body.drawer-open { overflow: hidden; }
a { color: var(--primary); text-decoration: none; }
button, input, select, textarea { font: inherit; }
button, a { -webkit-tap-highlight-color: transparent; }
button:focus-visible, a:focus-visible, input:focus-visible, select:focus-visible, textarea:focus-visible, summary:focus-visible {
  outline: 3px solid color-mix(in srgb, var(--primary) 42%, transparent);
  outline-offset: 2px;
}
code, pre { font-family: "Roboto Mono", ui-monospace, monospace; }
code { font-size: 12px; overflow-wrap: anywhere; }
h1, h2, h3, p { margin-top: 0; }
h1 { font-size: 22px; line-height: 1.3; font-weight: 400; }
h2 { font-size: 16px; line-height: 1.4; font-weight: 500; }
h3 { font-size: 14px; font-weight: 500; }
p { color: var(--on-surface-variant); }
.material-symbols-outlined {
  font-family: "Material Symbols Outlined";
  font-weight: normal;
  font-style: normal;
  font-size: 22px;
  line-height: 1;
  letter-spacing: normal;
  text-transform: none;
  display: inline-block;
  white-space: nowrap;
  word-wrap: normal;
  direction: ltr;
  -webkit-font-feature-settings: "liga";
  -webkit-font-smoothing: antialiased;
  font-feature-settings: "liga";
}
.skip-link {
  position: fixed;
  z-index: 1000;
  top: 8px;
  left: 8px;
  padding: 8px 14px;
  border-radius: 20px;
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
  border-radius: 6px;
  background: var(--secondary-container);
  color: var(--on-secondary-container);
  font-size: 10px;
  font-weight: 700;
  letter-spacing: .08em;
}
.icon-button {
  width: 40px;
  height: 40px;
  display: inline-grid;
  place-items: center;
  padding: 0;
  border: 0;
  border-radius: 50%;
  background: transparent;
  color: var(--on-surface-variant);
  cursor: pointer;
}
.icon-button:hover { background: var(--surface-high); color: var(--on-surface); }
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
  border-radius: 24px;
  background: transparent;
  color: var(--on-surface-variant);
  font-size: 14px;
  font-weight: 500;
  text-align: left;
  cursor: pointer;
  transition: background .2s, color .2s;
}
.nav-link:hover { background: var(--surface-high); color: var(--on-surface); }
.nav-link.active { background: var(--secondary-container); color: var(--on-secondary-container); }
.nav-link.active .material-symbols-outlined { font-variation-settings: "FILL" 1; }
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
.page-header h1, .page-title { margin: 0; color: var(--on-surface); font-size: 22px; font-weight: 400; }
.page-header p { margin: 4px 0 0; font-size: 13px; }
.count-badge {
  display: inline-flex;
  align-items: center;
  min-height: 28px;
  padding: 2px 10px;
  border-radius: 14px;
  background: var(--secondary-container);
  color: var(--on-secondary-container);
  font-size: 12px;
  font-weight: 500;
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
  padding: var(--spacing);
  border: 1px solid var(--outline-variant);
  border-radius: 12px;
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
  padding: 14px 16px;
  border: 1px solid var(--outline-variant);
  border-radius: 12px;
  background: var(--surface);
}
.metrics span, .metrics small { display: block; color: var(--on-surface-variant); }
.metrics span { font-size: 12px; font-weight: 500; }
.metrics strong { display: block; margin: 8px 0 4px; font-size: 26px; line-height: 1.1; font-weight: 500; }
.metrics small { min-height: 18px; font-size: 11px; }

.table-wrap {
  width: 100%;
  overflow: auto;
  border: 1px solid var(--outline-variant);
  border-radius: 8px;
}
table { width: 100%; min-width: 760px; border-collapse: collapse; text-align: left; }
th, td { padding: 10px 12px; border-bottom: 1px solid var(--outline-variant); vertical-align: middle; }
th {
  position: sticky;
  top: 0;
  z-index: 1;
  background: var(--surface-container);
  color: var(--on-surface-variant);
  font-size: 12px;
  font-weight: 500;
  white-space: nowrap;
}
td { max-width: 300px; font-size: 13px; }
tbody tr:last-child td { border-bottom: 0; }
tbody tr:hover td { background: var(--surface-low); }
.secondary { color: var(--on-surface-variant); }
.nowrap { white-space: nowrap; }
.positive { color: var(--success); font-weight: 500; }
.negative, .error-cell { color: var(--error); }
.wrap-cell { min-width: 220px; white-space: normal; overflow-wrap: anywhere; }
.cell-note { display: block; margin-top: 2px; color: var(--outline); font-size: 11px; font-weight: 400; }
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
.table-empty { padding: 32px; color: var(--on-surface-variant); text-align: center; }
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
  min-height: 22px;
  padding: 2px 8px;
  border-radius: 6px;
  font-size: 12px;
  font-weight: 500;
  white-space: nowrap;
}
.status.success { color: var(--success); background: var(--success-container); }
.status.warning { color: var(--warning); background: var(--warning-container); }
.status.danger { color: var(--error); background: var(--error-container); }
.status.info { color: var(--info); background: var(--info-container); }
.status.neutral { color: var(--on-surface-variant); background: var(--surface-highest); }

.filter-bar {
  display: flex;
  align-items: end;
  flex-wrap: wrap;
  gap: 12px;
  margin-bottom: var(--spacing);
  padding: var(--spacing);
  border: 1px solid var(--outline-variant);
  border-radius: 12px;
  background: var(--surface);
}
.filter-bar label {
  min-width: 150px;
  display: grid;
  gap: 5px;
  color: var(--on-surface-variant);
  font-size: 12px;
  font-weight: 500;
}
.filter-bar .search-field { flex: 1; min-width: 240px; }
.filter-bar.resource-filters { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); }
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
  border-radius: 18px;
  color: var(--primary);
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  list-style: none;
}
.filter-more summary::-webkit-details-marker { display: none; }
.filter-more summary:hover { background: var(--surface-high); }
.filter-more summary .material-symbols-outlined { font-size: 18px; }
.filter-more-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: 12px;
  margin-top: 12px;
  padding: 12px;
  border-radius: 8px;
  background: var(--surface-low);
}
.filter-actions { display: flex; align-items: center; justify-content: flex-end; gap: 8px; }
input, select, textarea {
  width: 100%;
  border: 1px solid var(--outline);
  border-radius: 4px;
  outline: 0;
  background: transparent;
  color: var(--on-surface);
  padding: 8px 12px;
}
input, select { min-height: 44px; }
textarea { min-height: 84px; resize: vertical; line-height: 1.5; }
input:focus, select:focus, textarea:focus { border: 2px solid var(--primary); padding: 7px 11px; }
input[type="checkbox"] {
  width: 18px;
  height: 18px;
  min-height: 0;
  padding: 0;
  accent-color: var(--primary);
}
input[type="checkbox"]:focus { padding: 0; }
.filter-reset { align-self: center; padding: 10px 4px; }

.filled-button, .outlined-button, .full-button {
  min-height: 40px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 8px 20px;
  border: 1px solid transparent;
  border-radius: 20px;
  font-size: 14px;
  font-weight: 500;
  cursor: pointer;
  white-space: nowrap;
}
.filled-button { background: var(--primary); color: var(--on-primary); }
.outlined-button { border-color: var(--outline); background: transparent; color: var(--primary); }
.outlined-button.danger { color: var(--error); }
.filled-button:hover { box-shadow: inset 0 0 0 999px rgb(255 255 255 / .08); }
.outlined-button:hover { background: var(--surface-high); }
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

.notice { margin-bottom: var(--spacing); padding: 10px 14px; border-radius: 8px; font-size: 13px; }
.notice.success { color: var(--success); background: var(--success-container); }
.notice.danger { color: var(--on-error-container); background: var(--error-container); }
.form-error-summary {
  grid-column: 1 / -1;
  width: 100%;
  margin-bottom: 12px;
  padding: 10px 12px;
  border-left: 4px solid var(--error);
  border-radius: 4px;
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
.diagnostic { margin: 0; font-family: "Roboto Mono", monospace; overflow-wrap: anywhere; }

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
.decision-form label, .reply-form label {
  display: grid;
  gap: 6px;
  color: var(--on-surface-variant);
  font-size: 12px;
  font-weight: 500;
}
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
.sticky-review-decision { position: sticky; top: 82px; display: grid; gap: 16px; }
.sticky-review-decision form { display: grid; gap: 14px; }
.sticky-review-decision hr { width: 100%; border: 0; border-top: 1px solid var(--outline-variant); }
.decision-actions { display: grid; gap: 8px; }
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
.inline-editor > form { position: absolute; z-index: 5; right: 0; top: calc(100% + 8px); width: min(420px, 90vw); padding: 16px; border: 1px solid var(--outline-variant); border-radius: 12px; background: var(--surface-container-high); box-shadow: var(--shadow-2); display: grid; gap: 12px; }
.review-inline-editor > summary { display: flex; align-items: center; gap: 8px; cursor: pointer; list-style: none; }
.review-inline-editor > summary span:last-child { margin-left: auto; color: var(--outline); font-size: 12px; font-weight: 400; }
.review-inline-editor > form { margin-top: 20px; }
.comparison-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; margin-top: 16px; }
.binding-row { align-items: start; }
.binding-details { display: grid; gap: 8px; }
.binding-details > div { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.binding-details span { min-width: 0; display: grid; gap: 2px; }
.binding-actions { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 8px; }
.inline-role-form { display: inline-flex; align-items: center; gap: 6px; flex-wrap: wrap; }

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
.composer-form label, .blog-fields label, .writing-grid label { display: grid; gap: 6px; color: var(--on-surface-variant); font-size: 12px; font-weight: 500; }
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
.composer-empty { padding: 24px; border: 1px dashed var(--outline-variant); border-radius: 10px; color: var(--outline); text-align: center; }

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
.report-form label { display: grid; align-content: start; gap: 6px; color: var(--on-surface-variant); font-size: 12px; font-weight: 500; }

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
.choice-card:has(input:checked) { border-color: var(--primary); background: var(--primary-container); color: var(--on-primary-container); }
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
  padding: 32px;
  border: 1px solid var(--outline-variant);
  border-radius: 12px;
  background: var(--surface);
  text-align: center;
}
.empty-mark {
  width: 48px;
  height: 48px;
  display: grid;
  place-items: center;
  margin-bottom: 12px;
  border-radius: 14px;
  background: var(--primary-container);
  color: var(--on-primary-container);
  font-size: 20px;
  font-weight: 500;
}
.empty-state h2 { font-size: 16px; }
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
.standalone-icon .material-symbols-outlined { font-size: 28px; }
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
.dialog-icon .material-symbols-outlined { font-size: 28px; }
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
  .comparison-grid { grid-template-columns: 1fr; }
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
