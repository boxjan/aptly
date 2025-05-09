package deb

import (
	gocontext "context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/aptly-dev/aptly/aptly"
	"github.com/aptly-dev/aptly/utils"
	"github.com/rs/zerolog/log"
)

// Package is single instance of Debian package
type Package struct {
	// Basic package properties
	Name         string
	Version      string
	Architecture string
	// If this source package, this field holds "real" architecture value,
	// while Architecture would be equal to "source"
	SourceArchitecture string
	// For binary package, name of source package
	Source string
	// List of virtual packages this package provides
	Provides []string
	// Hash of files section
	FilesHash uint64
	// Is this package a dummy installer package
	IsInstaller bool
	// Is this source package
	IsSource bool
	// Is this udeb package
	IsUdeb bool
	// Is this >= 0.6 package?
	V06Plus bool
	// Offload fields
	deps     *PackageDependencies
	extra    *Stanza
	files    *PackageFiles
	contents []string
	// Mother collection
	collection *PackageCollection
}

// Package types
const (
	PackageTypeBinary    = "deb"
	PackageTypeUdeb      = "udeb"
	PackageTypeSource    = "source"
	PackageTypeInstaller = "installer"
)

// Special architectures
const (
	ArchitectureAll    = "all"
	ArchitectureAny    = "any"
	ArchitectureSource = "source"
)

// Check interface
var (
	_ json.Marshaler = &Package{}
)

// NewPackageFromControlFile creates Package from parsed Debian control file
func NewPackageFromControlFile(input Stanza) *Package {
	result := &Package{
		Name:         input["Package"],
		Version:      input["Version"],
		Architecture: input["Architecture"],
		Source:       input["Source"],
		V06Plus:      true,
	}

	delete(input, "Package")
	delete(input, "Version")
	delete(input, "Architecture")
	delete(input, "Source")

	filesize, _ := strconv.ParseInt(input["Size"], 10, 64)

	md5, ok := input["MD5sum"]
	if !ok {
		// there are some broken repos out there with MD5 in wrong field
		md5 = input["MD5Sum"]
	}

	result.UpdateFiles(PackageFiles{PackageFile{
		Filename:     filepath.Base(input["Filename"]),
		downloadPath: filepath.Dir(input["Filename"]),
		Checksums: utils.ChecksumInfo{
			Size:   filesize,
			MD5:    strings.TrimSpace(md5),
			SHA1:   strings.TrimSpace(input["SHA1"]),
			SHA256: strings.TrimSpace(input["SHA256"]),
			SHA512: strings.TrimSpace(input["SHA512"]),
		},
	}})

	delete(input, "Filename")
	delete(input, "MD5sum")
	delete(input, "MD5Sum")
	delete(input, "SHA1")
	delete(input, "SHA256")
	delete(input, "SHA512")
	delete(input, "Size")

	depends := &PackageDependencies{}
	depends.Depends = parseDependencies(input, "Depends")
	depends.PreDepends = parseDependencies(input, "Pre-Depends")
	depends.Suggests = parseDependencies(input, "Suggests")
	depends.Recommends = parseDependencies(input, "Recommends")
	result.deps = depends

	result.Provides = parseDependencies(input, "Provides")

	result.extra = &input

	return result
}

// NewSourcePackageFromControlFile creates Package from parsed Debian control file for source package
func NewSourcePackageFromControlFile(input Stanza) (*Package, error) {
	result := &Package{
		IsSource:           true,
		Name:               input["Package"],
		Version:            input["Version"],
		Architecture:       "source",
		SourceArchitecture: input["Architecture"],
		V06Plus:            true,
	}

	delete(input, "Package")
	delete(input, "Version")
	delete(input, "Architecture")

	var err error

	files := make(PackageFiles, 0, 3)
	files, err = files.ParseSumFields(input)
	if err != nil {
		return nil, err
	}

	delete(input, "Files")
	delete(input, "Checksums-Sha1")
	delete(input, "Checksums-Sha256")

	for i := range files {
		files[i].downloadPath = input["Directory"]
	}

	result.UpdateFiles(files)

	depends := &PackageDependencies{}
	depends.BuildDepends = parseDependencies(input, "Build-Depends")
	depends.BuildDependsInDep = parseDependencies(input, "Build-Depends-Indep")
	result.deps = depends

	result.extra = &input

	return result, nil
}

// NewUdebPackageFromControlFile creates .udeb Package from parsed Debian control file
func NewUdebPackageFromControlFile(input Stanza) *Package {
	p := NewPackageFromControlFile(input)
	p.IsUdeb = true

	return p
}

// NewInstallerPackageFromControlFile creates a dummy installer Package from parsed hash sum file
func NewInstallerPackageFromControlFile(input Stanza, repo *RemoteRepo, component, architecture string, d aptly.Downloader) (*Package, error) {
	p := &Package{
		Name:         "installer",
		Architecture: architecture,
		IsInstaller:  true,
		V06Plus:      true,
		extra:        &Stanza{},
		deps:         &PackageDependencies{},
	}

	files := make(PackageFiles, 0)
	files, err := files.ParseSumField(input[""], func(sum *utils.ChecksumInfo, data string) { sum.SHA256 = data }, false, false)
	if err != nil {
		return nil, err
	}

	var relPath string
	if repo.Distribution == aptly.DistributionFocal {
		relPath = filepath.Join("dists", repo.Distribution, component, fmt.Sprintf("%s-%s", p.Name, architecture), "current", "legacy-images")
	} else {
		relPath = filepath.Join("dists", repo.Distribution, component, fmt.Sprintf("%s-%s", p.Name, architecture), "current", "images")
	}
	for i := range files {
		files[i].downloadPath = relPath

		url := repo.PackageURL(files[i].DownloadURL()).String()
		var size int64
		size, err = d.GetLength(gocontext.TODO(), url)
		if err != nil {
			return nil, err
		}

		files[i].Checksums.Size = size
	}

	p.UpdateFiles(files)
	return p, nil
}

// Key returns unique key identifying package
func (p *Package) Key(prefix string) []byte {
	if p.V06Plus {
		return []byte(fmt.Sprintf("%sP%s %s %s %08x", prefix, p.Architecture, p.Name, p.Version, p.FilesHash))
	}

	return p.ShortKey(prefix)
}

// ShortKey returns key for the package that should be unique in one list
func (p *Package) ShortKey(prefix string) []byte {
	return []byte(fmt.Sprintf("%sP%s %s %s", prefix, p.Architecture, p.Name, p.Version))
}

// String creates readable representation
func (p *Package) String() string {
	return fmt.Sprintf("%s_%s_%s", p.Name, p.Version, p.Architecture)
}

// ExtendedStanza returns package stanza enhanced with aptly-specific fields
func (p *Package) ExtendedStanza() Stanza {
	stanza := p.Stanza()
	stanza["FilesHash"] = fmt.Sprintf("%08x", p.FilesHash)
	stanza["Key"] = string(p.Key(""))
	stanza["ShortKey"] = string(p.ShortKey(""))

	return stanza
}

// MarshalJSON implements json.Marshaller interface
func (p *Package) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.ExtendedStanza())
}

// GetField returns fields from package
func (p *Package) GetField(name string) string {
	switch name {
	// $Version is handled in FieldQuery
	case "$Source":
		if p.IsSource {
			return ""
		}
		source := p.Source
		if source == "" {
			return p.Name
		} else if pos := strings.Index(source, "("); pos != -1 {
			return strings.TrimSpace(source[:pos])
		}
		return source
	case "$SourceVersion":
		if p.IsSource {
			return ""
		}
		source := p.Source
		if pos := strings.Index(source, "("); pos != -1 {
			if pos2 := strings.LastIndex(source, ")"); pos2 != -1 && pos2 > pos {
				return strings.TrimSpace(source[pos+1 : pos2])
			}
		}
		return p.Version
	case "$Architecture":
		return p.Architecture
	case "$PackageType":
		if p.IsSource {
			return PackageTypeSource
		}
		if p.IsUdeb {
			return PackageTypeUdeb
		}
		return PackageTypeBinary
	case "Name":
		return p.Name
	case "Version":
		return p.Version
	case "Architecture":
		if p.IsSource {
			return p.SourceArchitecture
		}
		return p.Architecture
	case "Source":
		return p.Source
	case "Depends":
		return strings.Join(p.Deps().Depends, ", ")
	case "Pre-Depends":
		return strings.Join(p.Deps().PreDepends, ", ")
	case "Suggests":
		return strings.Join(p.Deps().Suggests, ", ")
	case "Recommends":
		return strings.Join(p.Deps().Recommends, ", ")
	case "Provides":
		return strings.Join(p.Provides, ", ")
	case "Build-Depends":
		return strings.Join(p.Deps().BuildDepends, ", ")
	case "Build-Depends-Indep":
		return strings.Join(p.Deps().BuildDependsInDep, ", ")
	default:
		return p.Extra()[name]
	}
}

// ProvidedPackages returns just the package names of the provided packages (without version numbers such as
// `(= 1.2.3)`).
func (p *Package) ProvidedPackages() []string {
	result := make([]string, len(p.Provides))
	for i, provided := range p.Provides {
		providedDep, err := ParseDependency(provided)
		if err != nil {
			// Should never happen, but I included this, so it definitely has the old behavior in case there is no
			// special syntax.
			result[i] = provided
		} else {
			result[i] = providedDep.Pkg
		}
	}
	return result
}

// MatchesArchitecture checks whether packages matches specified architecture
func (p *Package) MatchesArchitecture(arch string) bool {
	if p.Architecture == ArchitectureAll && arch != ArchitectureSource {
		return true
	}

	return p.Architecture == arch
}

func JoinErrors(errs ...error) error {
	var combinedErr error
	for _, err := range errs {
		if err != nil {
			if combinedErr == nil {
				combinedErr = err
			} else {
				combinedErr = fmt.Errorf("%w\n%v", combinedErr, err)
			}
		}
	}
	return combinedErr
}

// providesDependency checks if the package `Provide:`s the dependency, assuming that the architecture matches.
// If the `Provides:` entry includes a version number, it will be considered when checking the dependency.
func (p *Package) providesDependency(dep Dependency) (bool, error) {
	var errs []error // won't cause an allocation in case of no errors
	for _, provided := range p.Provides {
		providedDep, err := ParseDependency(provided)
		if err != nil {
			errs = append(errs, err)
		}
		if providedDep.Relation != VersionEqual && providedDep.Relation != VersionDontCare {
			// The only relation allowed here is `=`.
			// > The relations allowed are [...]. The exception is the Provides field, for which only = is allowed.
			// > [...]
			// > A Provides field may contain version numbers, and such a version number will be considered when
			// > considering a dependency on or conflict with the virtual package name.
			// -- https://www.debian.org/doc/debian-policy/ch-relationships.html
			errs = append(errs, fmt.Errorf("unsupported relation in Provides: %s", providedDep.String()))
			continue
		}
		providedVersion := providedDep.Version
		if providedVersion == "" {
			providedVersion = p.Version
		}
		if providedDep.Pkg == dep.Pkg && versionSatisfiesDependency(providedVersion, dep) {
			return true, nil
		}
	}
	return false, JoinErrors(errs...)
}

// MatchesDependency checks whether package matches specified dependency
func (p *Package) MatchesDependency(dep Dependency) bool {
	if dep.Architecture != "" && !p.MatchesArchitecture(dep.Architecture) {
		return false
	}

	providesDep, err := p.providesDependency(dep)
	if err != nil {
		log.Warn().Err(err).Msg("Error while checking if package provides dependency")
	}
	if providesDep {
		return true
	}

	if dep.Pkg != p.Name {
		return false
	}

	return versionSatisfiesDependency(p.Version, dep)
}

func versionSatisfiesDependency(version string, dep Dependency) bool {
	r := CompareVersions(version, dep.Version)

	switch dep.Relation {
	case VersionEqual:
		return r == 0
	case VersionLess:
		return r < 0
	case VersionGreater:
		return r > 0
	case VersionLessOrEqual:
		return r <= 0
	case VersionGreaterOrEqual:
		return r >= 0
	case VersionPatternMatch:
		matched, err := filepath.Match(dep.Version, version)
		return err == nil && matched
	case VersionRegexp:
		return dep.Regexp.FindStringIndex(version) != nil
	case VersionDontCare:
		return true
	default:
		panic(fmt.Sprintf("unknown relation: %d", dep.Relation))
	}
}

// GetName returns package name
func (p *Package) GetName() string {
	return p.Name
}

// GetFullName returns the package full name
func (p *Package) GetFullName() string {
	return strings.Join([]string{p.Name, p.Version, p.Architecture}, "_")
}

// GetVersion returns package version
func (p *Package) GetVersion() string {
	return p.Version
}

// GetArchitecture returns package arch
func (p *Package) GetArchitecture() string {
	return p.Architecture
}

// GetDependencies compiles list of dependencies by flags from options
func (p *Package) GetDependencies(options int) (dependencies []string) {
	deps := p.Deps()

	dependencies = make([]string, 0, 30)
	dependencies = append(dependencies, deps.Depends...)
	dependencies = append(dependencies, deps.PreDepends...)

	if options&DepFollowRecommends == DepFollowRecommends {
		dependencies = append(dependencies, deps.Recommends...)
	}

	if options&DepFollowSuggests == DepFollowSuggests {
		dependencies = append(dependencies, deps.Suggests...)
	}

	if options&DepFollowBuild == DepFollowBuild {
		dependencies = append(dependencies, deps.BuildDepends...)
		dependencies = append(dependencies, deps.BuildDependsInDep...)
	}

	if options&DepFollowSource == DepFollowSource {
		source := p.Source
		if source == "" {
			source = p.Name
		}
		if strings.Contains(source, ")") {
			dependencies = append(dependencies, fmt.Sprintf("%s {source}", source))
		} else {
			dependencies = append(dependencies, fmt.Sprintf("%s (= %s) {source}", source, p.Version))
		}
	}

	return
}

// QualifiedName returns [$SECTION/]$NAME
func (p *Package) QualifiedName() string {
	section := p.Extra()["Section"]
	if section != "" {
		return section + "/" + p.Name
	}

	return p.Name
}

// Extra returns Stanza of extra fields (it may load it from collection)
func (p *Package) Extra() Stanza {
	if p.extra == nil {
		if p.collection == nil {
			panic("extra == nil && collection == nil")
		}
		p.extra = p.collection.loadExtra(p)
	}

	return *p.extra
}

// Deps returns parsed package dependencies (it may load it from collection)
func (p *Package) Deps() *PackageDependencies {
	if p.deps == nil {
		if p.collection == nil {
			panic("deps == nil && collection == nil")
		}

		p.deps = p.collection.loadDependencies(p)
	}

	return p.deps
}

// Files returns parsed files records (it may load it from collection)
func (p *Package) Files() PackageFiles {
	if p.files == nil {
		if p.collection == nil {
			panic("files == nil && collection == nil")
		}

		p.files = p.collection.loadFiles(p)
	}

	return *p.files
}

// Contents returns cached package contents
func (p *Package) Contents(packagePool aptly.PackagePool, progress aptly.Progress) []string {
	if p.IsSource {
		return nil
	}

	return p.collection.loadContents(p, packagePool, progress)
}

// CalculateContents looks up contents in package file
func (p *Package) CalculateContents(packagePool aptly.PackagePool, progress aptly.Progress) ([]string, error) {
	if p.IsSource {
		return nil, nil
	}

	file := p.Files()[0]
	poolPath, err := file.GetPoolPath(packagePool)
	if err != nil {
		if progress != nil {
			progress.ColoredPrintf("@y[!]@| @!Failed to build pool path: @| %s", err)
		}
		return nil, err
	}

	reader, err := packagePool.Open(poolPath)
	if err != nil {
		if progress != nil {
			progress.ColoredPrintf("@y[!]@| @!Failed to open package in pool: @| %s", err)
		}
		return nil, err
	}
	defer func() { _ = reader.Close() }()

	contents, err := GetContentsFromDeb(reader, file.Filename)
	if err != nil {
		if progress != nil {
			progress.ColoredPrintf("@y[!]@| @!Failed to generate package contents: @| %s", err)
		}
		return nil, err
	}

	return contents, nil
}

// UpdateFiles saves new state of files
func (p *Package) UpdateFiles(files PackageFiles) {
	p.files = &files
	p.FilesHash = files.Hash()
}

// Stanza creates original stanza from package
func (p *Package) Stanza() (result Stanza) {
	result = p.Extra().Copy()
	result["Package"] = p.Name
	result["Version"] = p.Version

	if p.IsSource {
		result["Architecture"] = p.SourceArchitecture
	} else {
		result["Architecture"] = p.Architecture
		if p.Source != "" {
			result["Source"] = p.Source
		}
	}

	if p.IsSource {
		md5, sha1, sha256, sha512 := []string{}, []string{}, []string{}, []string{}

		for _, f := range p.Files() {
			if f.Checksums.MD5 != "" {
				md5 = append(md5, fmt.Sprintf(" %s %d %s\n", f.Checksums.MD5, f.Checksums.Size, f.Filename))
			}
			if f.Checksums.SHA1 != "" {
				sha1 = append(sha1, fmt.Sprintf(" %s %d %s\n", f.Checksums.SHA1, f.Checksums.Size, f.Filename))
			}
			if f.Checksums.SHA256 != "" {
				sha256 = append(sha256, fmt.Sprintf(" %s %d %s\n", f.Checksums.SHA256, f.Checksums.Size, f.Filename))
			}
			if f.Checksums.SHA512 != "" {
				sha512 = append(sha512, fmt.Sprintf(" %s %d %s\n", f.Checksums.SHA512, f.Checksums.Size, f.Filename))
			}
		}

		result["Files"] = strings.Join(md5, "")
		if len(sha1) > 0 {
			result["Checksums-Sha1"] = strings.Join(sha1, "")
		}
		if len(sha256) > 0 {
			result["Checksums-Sha256"] = strings.Join(sha256, "")
		}
		if len(sha512) > 0 {
			result["Checksums-Sha512"] = strings.Join(sha512, "")
		}
	} else if p.IsInstaller {
		sha256 := []string{}
		for _, f := range p.Files() {
			sha256 = append(sha256, fmt.Sprintf("%s  %s", f.Checksums.SHA256, f.Filename))
		}
		result[""] = strings.Join(sha256, "\n")
	} else {
		f := p.Files()[0]
		result["Filename"] = f.DownloadURL()
		if f.Checksums.MD5 != "" {
			result["MD5sum"] = f.Checksums.MD5
		}
		if f.Checksums.SHA1 != "" {
			result["SHA1"] = f.Checksums.SHA1
		}
		if f.Checksums.SHA256 != "" {
			result["SHA256"] = f.Checksums.SHA256
		}
		if f.Checksums.SHA512 != "" {
			result["SHA512"] = f.Checksums.SHA512
		}
		result["Size"] = fmt.Sprintf("%d", f.Checksums.Size)
	}

	deps := p.Deps()

	if deps.Depends != nil {
		result["Depends"] = strings.Join(deps.Depends, ", ")
	}
	if deps.PreDepends != nil {
		result["Pre-Depends"] = strings.Join(deps.PreDepends, ", ")
	}
	if deps.Suggests != nil {
		result["Suggests"] = strings.Join(deps.Suggests, ", ")
	}
	if deps.Recommends != nil {
		result["Recommends"] = strings.Join(deps.Recommends, ", ")
	}
	if p.Provides != nil {
		result["Provides"] = strings.Join(p.Provides, ", ")
	}
	if deps.BuildDepends != nil {
		result["Build-Depends"] = strings.Join(deps.BuildDepends, ", ")
	}
	if deps.BuildDependsInDep != nil {
		result["Build-Depends-Indep"] = strings.Join(deps.BuildDependsInDep, ", ")
	}

	return
}

// Equals compares two packages to be identical
func (p *Package) Equals(p2 *Package) bool {
	return p.Name == p2.Name && p.Version == p2.Version && p.SourceArchitecture == p2.SourceArchitecture &&
		p.Architecture == p2.Architecture && p.Source == p2.Source && p.IsSource == p2.IsSource &&
		p.FilesHash == p2.FilesHash
}

// LinkFromPool links package file from pool to dist's pool location
func (p *Package) LinkFromPool(publishedStorage aptly.PublishedStorage, packagePool aptly.PackagePool,
	prefix, relPath string, force bool) error {

	for i, f := range p.Files() {
		sourcePoolPath, err := f.GetPoolPath(packagePool)
		if err != nil {
			return err
		}

		err = publishedStorage.LinkFromPool(prefix, relPath, f.Filename, packagePool, sourcePoolPath, f.Checksums, force)
		if err != nil {
			return err
		}

		if p.IsSource {
			p.Extra()["Directory"] = relPath
		} else {
			p.Files()[i].downloadPath = relPath
		}
	}

	return nil
}

// PoolDirectory returns directory in package pool of published repository for this package files
func (p *Package) PoolDirectory() (string, error) {
	source := p.Source
	if source == "" {
		source = p.Name
	} else if pos := strings.Index(source, "("); pos != -1 {
		source = strings.TrimSpace(source[:pos])
	}

	if len(source) < 2 {
		return "", fmt.Errorf("package source %s too short", source)
	}

	var subdir string
	if strings.HasPrefix(source, "lib") {
		subdir = source[:4]
	} else {
		subdir = source[:1]

	}

	return filepath.Join(subdir, source), nil
}

// PackageDownloadTask is a element of download queue for the package
type PackageDownloadTask struct {
	File         *PackageFile
	Additional   []PackageDownloadTask
	TempDownPath string
	Done         bool
}

// DownloadList returns list of missing package files for download in format
// [[srcpath, dstpath]]
func (p *Package) DownloadList(packagePool aptly.PackagePool, checksumStorage aptly.ChecksumStorage) (result []PackageDownloadTask, err error) {
	result = make([]PackageDownloadTask, 0, 1)

	files := p.Files()
	for idx := range files {
		verified, err := files[idx].Verify(packagePool, checksumStorage)
		if err != nil {
			return nil, err
		}

		if !verified {
			result = append(result, PackageDownloadTask{File: &files[idx]})
		}
	}

	return result, nil
}

// VerifyFiles verifies that all package files have neen correctly downloaded
func (p *Package) VerifyFiles(packagePool aptly.PackagePool, checksumStorage aptly.ChecksumStorage) (result bool, err error) {
	result = true

	for _, f := range p.Files() {
		result, err = f.Verify(packagePool, checksumStorage)
		if err != nil || !result {
			return
		}
	}

	return
}

// FilepathList returns list of paths to files in package repository
func (p *Package) FilepathList(packagePool aptly.PackagePool) ([]string, error) {
	var err error
	result := make([]string, len(p.Files()))

	for i, f := range p.Files() {
		result[i], err = f.GetPoolPath(packagePool)
		if err != nil {
			return nil, err
		}
	}

	return result, nil
}
