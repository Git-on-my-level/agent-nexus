# UI extensions

The standalone UI is one workspace, with native authentication and no commercial
routes or plan catalog. Account profile and external provider defaults are inert.

`web-ui/scripts/compose-ui.mjs` exports `composeUi({ sourceRoot, extensionRoot,
outputRoot })`. An extension supplies `extension.json` with explicit `replace`
file paths and `add` directory/file prefixes. Source, tests and static assets are
copied into an output tree outside both inputs. Undeclared replacements fail
before composition; neither input tree is changed. Install OSS dependencies first.
The composition uses the existing workspace shell, routes and build configuration.

The supported replacement seams are `src/lib/server/extensions/provider.js`
(the existing out-of-workspace provider contract), `src/lib/extensions/accountSession.js`
(optional profile state), `src/lib/extensions/launchFlow.js` (external sign-in
destination) and `src/lib/extensions/supportLink.js` (contact policy).
External routes and components are additive. Shell capability fields supply
account, chooser, recovery and people destinations/labels; the workspace shell owns layout and
workspace authentication. Extensions must preserve that ownership and keep core
independent of external services.

Private deployment tooling owns composition, build, tests and serving the composed
output. Generated extension sources must never be written back into the OSS source
tree. A standalone build always runs `scripts/check-commercial-boundary.mjs` on
source and both server/client output. CI fails on forbidden route directories,
commercial imports, plan identifiers, price objects and distinctive plan copy.
