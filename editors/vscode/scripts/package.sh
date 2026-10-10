#!/bin/sh
# Packages the extension as a .vsix with nothing but zip: a .vsix is a zip
# archive holding extension.vsixmanifest, [Content_Types].xml and the
# extension's files under extension/. No npm packages are needed; vsce
# produces an equivalent file if you have it.
#
# Usage: scripts/package.sh [output.vsix]
# Install: code --install-extension plutonium-<version>.vsix
set -eu

cd "$(dirname "$0")/.."

field() {
	sed -n "s/^  \"$1\": \"\\(.*\\)\",\$/\\1/p" package.json
}
name=$(field name)
publisher=$(field publisher)
version=$(field version)
display=$(field displayName)
description=$(field description)
engine=$(sed -n 's/^    "vscode": "\(.*\)"$/\1/p' package.json)
if [ -z "$name" ] || [ -z "$publisher" ] || [ -z "$version" ] || [ -z "$engine" ]; then
	echo "package.sh: cannot read name, publisher, version or engines.vscode from package.json" >&2
	exit 1
fi
out=${1:-$name-$version.vsix}
case $out in
/*) ;;
*) out=$PWD/$out ;;
esac

stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
mkdir -p "$stage/extension"
cp -R package.json extension.js language-configuration.json README.md src syntaxes "$stage/extension/"

cat >"$stage/extension.vsixmanifest" <<EOF
<?xml version="1.0" encoding="utf-8"?>
<PackageManifest Version="2.0.0" xmlns="http://schemas.microsoft.com/developer/vsx-schema/2011" xmlns:d="http://schemas.microsoft.com/developer/vsx-schema-design/2011">
	<Metadata>
		<Identity Language="en-US" Id="$name" Version="$version" Publisher="$publisher" />
		<DisplayName>$display</DisplayName>
		<Description xml:space="preserve">$description</Description>
		<Categories>Programming Languages,Formatters</Categories>
		<GalleryFlags>Public</GalleryFlags>
		<Properties>
			<Property Id="Microsoft.VisualStudio.Code.Engine" Value="$engine" />
			<Property Id="Microsoft.VisualStudio.Code.ExtensionDependencies" Value="" />
			<Property Id="Microsoft.VisualStudio.Code.ExtensionPack" Value="" />
			<Property Id="Microsoft.VisualStudio.Code.ExtensionKind" Value="workspace" />
			<Property Id="Microsoft.VisualStudio.Code.LocalizedLanguages" Value="" />
		</Properties>
	</Metadata>
	<Installation>
		<InstallationTarget Id="Microsoft.VisualStudio.Code" />
	</Installation>
	<Dependencies />
	<Assets>
		<Asset Type="Microsoft.VisualStudio.Code.Manifest" Path="extension/package.json" Addressable="true" />
		<Asset Type="Microsoft.VisualStudio.Services.Content.Details" Path="extension/README.md" Addressable="true" />
	</Assets>
</PackageManifest>
EOF

cat >"$stage/[Content_Types].xml" <<'EOF'
<?xml version="1.0" encoding="utf-8"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension=".json" ContentType="application/json" /><Default Extension=".js" ContentType="application/javascript" /><Default Extension=".md" ContentType="text/markdown" /><Default Extension=".vsixmanifest" ContentType="text/xml" /></Types>
EOF

rm -f "$out"
(cd "$stage" && zip -q -r -X -nw "$out" '[Content_Types].xml' extension.vsixmanifest extension)
echo "$out"
