package deb

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/aptly-dev/aptly/aptly"
	"github.com/aptly-dev/aptly/database"
	"github.com/aptly-dev/aptly/database/goleveldb"
	"github.com/aptly-dev/aptly/files"
	"github.com/ugorji/go/codec"

	. "gopkg.in/check.v1"
)

type pathExistsChecker struct {
	*CheckerInfo
}

var PathExists = &pathExistsChecker{
	&CheckerInfo{Name: "PathExists", Params: []string{"path"}},
}

func (checker *pathExistsChecker) Check(params []interface{}, names []string) (result bool, error string) {
	_, err := os.Stat(params[0].(string))
	return err == nil, ""
}

type NullSigner struct{}

func (n *NullSigner) Init() error {
	return nil
}

func (n *NullSigner) SetKey(keyRef string) {
}

func (n *NullSigner) SetBatch(batch bool) {
}

func (n *NullSigner) SetKeyRing(keyring, secretKeyring string) {
}

func (n *NullSigner) SetPassphrase(passphrase, passphraseFile string) {
}

func (n *NullSigner) DetachedSign(source string, destination string) error {
	return os.WriteFile(destination, []byte{}, 0644)
}

func (n *NullSigner) ClearSign(source string, destination string) error {
	return os.WriteFile(destination, []byte{}, 0644)
}

type FakeStorageProvider struct {
	storages map[string]aptly.PublishedStorage
}

func (p *FakeStorageProvider) GetPublishedStorage(name string) aptly.PublishedStorage {
	storage, ok := p.storages[name]
	if !ok {
		panic(fmt.Sprintf("unknown storage: %#v", name))
	}
	return storage
}

type PublishedRepoSuite struct {
	PackageListMixinSuite
	repo, repo2, repo3, repo4, repo5    *PublishedRepo
	root, root2                         string
	provider                            *FakeStorageProvider
	publishedStorage, publishedStorage2 *files.PublishedStorage
	packagePool                         aptly.PackagePool
	cs                                  aptly.ChecksumStorage
	localRepo                           *LocalRepo
	snapshot, snapshot2                 *Snapshot
	db                                  database.Storage
	factory                             *CollectionFactory
	packageCollection                   *PackageCollection
}

var _ = Suite(&PublishedRepoSuite{})

func (s *PublishedRepoSuite) SetUpTest(c *C) {
	s.SetUpPackages()

	s.db, _ = goleveldb.NewOpenDB(c.MkDir())
	s.factory = NewCollectionFactory(s.db)

	s.root = c.MkDir()
	s.publishedStorage = files.NewPublishedStorage(s.root, "", "")
	s.root2 = c.MkDir()
	s.publishedStorage2 = files.NewPublishedStorage(s.root2, "", "")
	s.provider = &FakeStorageProvider{map[string]aptly.PublishedStorage{
		"":            s.publishedStorage,
		"files:other": s.publishedStorage2}}
	s.packagePool = files.NewPackagePool(s.root, false)
	s.cs = files.NewMockChecksumStorage()

	tmpFilepath := filepath.Join(c.MkDir(), "file")
	c.Assert(os.WriteFile(tmpFilepath, nil, 0777), IsNil)

	var err error
	s.p1.Files()[0].PoolPath, err = s.packagePool.Import(tmpFilepath, s.p1.Files()[0].Filename, &s.p1.Files()[0].Checksums, false, s.cs)
	c.Assert(err, IsNil)

	s.p1.UpdateFiles(s.p1.Files())
	s.p2.UpdateFiles(s.p1.Files())
	s.p3.UpdateFiles(s.p1.Files())

	s.reflist = NewPackageRefListFromPackageList(s.list)

	repo, _ := NewRemoteRepo("yandex", "http://mirror.yandex.ru/debian/", "squeeze", []string{"main"}, []string{}, false, false, false)
	repo.packageRefs = s.reflist
	_ = s.factory.RemoteRepoCollection().Add(repo)

	s.localRepo = NewLocalRepo("local1", "comment1")
	s.localRepo.packageRefs = s.reflist
	_ = s.factory.LocalRepoCollection().Add(s.localRepo)

	s.snapshot, _ = NewSnapshotFromRepository("snap", repo)
	_ = s.factory.SnapshotCollection().Add(s.snapshot)

	s.snapshot2, _ = NewSnapshotFromRepository("snap", repo)
	_ = s.factory.SnapshotCollection().Add(s.snapshot2)

	s.packageCollection = s.factory.PackageCollection()
	_ = s.packageCollection.Update(s.p1)
	_ = s.packageCollection.Update(s.p2)
	_ = s.packageCollection.Update(s.p3)

	s.repo, _ = NewPublishedRepo("", "ppa", "squeeze", nil, []string{"main"}, []interface{}{s.snapshot}, s.factory, false)
	s.repo.SkipContents = true

	s.repo2, _ = NewPublishedRepo("", "ppa", "maverick", nil, []string{"main"}, []interface{}{s.localRepo}, s.factory, false)
	s.repo2.SkipContents = true

	s.repo3, _ = NewPublishedRepo("", "linux", "natty", nil, []string{"main", "contrib"}, []interface{}{s.snapshot, s.snapshot2}, s.factory, false)
	s.repo3.SkipContents = true

	s.repo4, _ = NewPublishedRepo("", "ppa", "maverick", []string{"source"}, []string{"main"}, []interface{}{s.localRepo}, s.factory, false)
	s.repo4.SkipContents = true

	s.repo5, _ = NewPublishedRepo("files:other", "ppa", "maverick", []string{"source"}, []string{"main"}, []interface{}{s.localRepo}, s.factory, false)
	s.repo5.SkipContents = true
}

func (s *PublishedRepoSuite) TearDownTest(c *C) {
	_ = s.db.Close()
}

func (s *PublishedRepoSuite) TestNewPublishedRepo(c *C) {
	c.Check(s.repo.sourceItems["main"].snapshot, Equals, s.snapshot)
	c.Check(s.repo.SourceKind, Equals, "snapshot")
	c.Check(s.repo.Sources["main"], Equals, s.snapshot.UUID)
	c.Check(s.repo.Components(), DeepEquals, []string{"main"})

	c.Check(s.repo2.sourceItems["main"].localRepo, Equals, s.localRepo)
	c.Check(s.repo2.SourceKind, Equals, "local")
	c.Check(s.repo2.Sources["main"], Equals, s.localRepo.UUID)
	c.Check(s.repo2.sourceItems["main"].packageRefs.Len(), Equals, 3)
	c.Check(s.repo2.Components(), DeepEquals, []string{"main"})

	c.Check(s.repo.RefList("main").Len(), Equals, 3)
	c.Check(s.repo2.RefList("main").Len(), Equals, 3)

	c.Check(s.repo3.Sources, DeepEquals, map[string]string{"main": s.snapshot.UUID, "contrib": s.snapshot2.UUID})
	c.Check(s.repo3.SourceKind, Equals, "snapshot")
	c.Check(s.repo3.sourceItems["main"].snapshot, Equals, s.snapshot)
	c.Check(s.repo3.sourceItems["contrib"].snapshot, Equals, s.snapshot2)
	c.Check(s.repo3.Components(), DeepEquals, []string{"contrib", "main"})

	c.Check(s.repo3.RefList("main").Len(), Equals, 3)
	c.Check(s.repo3.RefList("contrib").Len(), Equals, 3)

	c.Check(func() { _, _ = NewPublishedRepo("", ".", "a", nil, nil, nil, s.factory, false) }, PanicMatches, "publish with empty sources")
	c.Check(func() {
		_, _ = NewPublishedRepo("", ".", "a", nil, []string{"main"}, []interface{}{s.snapshot, s.snapshot2}, s.factory, false)
	}, PanicMatches, "sources and components should be equal in size")
	c.Check(func() {
		_, _ = NewPublishedRepo("", ".", "a", nil, []string{"main", "contrib"}, []interface{}{s.localRepo, s.snapshot2}, s.factory, false)
	}, PanicMatches, "interface conversion:.*")

	_, err := NewPublishedRepo("", ".", "a", nil, []string{"main", "main"}, []interface{}{s.snapshot, s.snapshot2}, s.factory, false)
	c.Check(err, ErrorMatches, "duplicate component name: main")

	_, err = NewPublishedRepo("", ".", "wheezy/updates", nil, []string{"main"}, []interface{}{s.snapshot}, s.factory, false)
	c.Check(err, IsNil)
}

func (s *PublishedRepoSuite) TestMultiDistPool(c *C) {
	repo, err := NewPublishedRepo("", "ppa", "squeeze", nil, []string{"main"}, []interface{}{s.snapshot}, s.factory, true)
	c.Assert(err, IsNil)
	err = repo.Publish(s.packagePool, s.provider, s.factory, &NullSigner{}, nil, false, "")
	c.Assert(err, IsNil)

	publishedStorage := files.NewPublishedStorage(s.root, "", "")

	c.Check(repo.Architectures, DeepEquals, []string{"i386"})

	rf, err := os.Open(filepath.Join(publishedStorage.PublicPath(), "ppa/dists/squeeze/Release"))
	c.Assert(err, IsNil)

	cfr := NewControlFileReader(rf, true, false)
	st, err := cfr.ReadStanza()
	c.Assert(err, IsNil)

	c.Check(st["Origin"], Equals, "ppa squeeze")
	c.Check(st["Components"], Equals, "main")
	c.Check(st["Architectures"], Equals, "i386")

	pf, err := os.Open(filepath.Join(publishedStorage.PublicPath(), "ppa/dists/squeeze/main/binary-i386/Packages"))
	c.Assert(err, IsNil)

	cfr = NewControlFileReader(pf, false, false)

	for i := 0; i < 3; i++ {
		st, err = cfr.ReadStanza()
		c.Assert(err, IsNil)

		c.Check(st["Filename"], Equals, "pool/squeeze/main/a/alien-arena/alien-arena-common_7.40-2_i386.deb")
	}

	st, err = cfr.ReadStanza()
	c.Assert(err, IsNil)
	c.Assert(st, IsNil)

	drf, err := os.Open(filepath.Join(publishedStorage.PublicPath(), "ppa/dists/squeeze/main/binary-i386/Release"))
	c.Assert(err, IsNil)

	cfr = NewControlFileReader(drf, true, false)
	st, err = cfr.ReadStanza()
	c.Assert(err, IsNil)

	c.Check(st["Archive"], Equals, "squeeze")
	c.Check(st["Architecture"], Equals, "i386")

	_, err = os.Stat(filepath.Join(publishedStorage.PublicPath(), "ppa/pool/squeeze/main/a/alien-arena/alien-arena-common_7.40-2_i386.deb"))
	c.Assert(err, IsNil)

}

func (s *PublishedRepoSuite) TestPrefixNormalization(c *C) {

	for _, t := range []struct {
		prefix        string
		expected      string
		errorExpected string
	}{
		{
			prefix:   "ppa",
			expected: "ppa",
		},
		{
			prefix:   "",
			expected: ".",
		},
		{
			prefix:   "/",
			expected: ".",
		},
		{
			prefix:   "//",
			expected: ".",
		},
		{
			prefix:   "//ppa/",
			expected: "ppa",
		},
		{
			prefix:   "ppa/..",
			expected: ".",
		},
		{
			prefix:   "ppa/ubuntu/",
			expected: "ppa/ubuntu",
		},
		{
			prefix:   "ppa/../ubuntu/",
			expected: "ubuntu",
		},
		{
			prefix:        "../ppa/",
			errorExpected: "invalid prefix .*",
		},
		{
			prefix:        "../ppa/../ppa/",
			errorExpected: "invalid prefix .*",
		},
		{
			prefix:        "ppa/dists",
			errorExpected: "invalid prefix .*",
		},
		{
			prefix:        "ppa/pool",
			errorExpected: "invalid prefix .*",
		},
	} {
		repo, err := NewPublishedRepo("", t.prefix, "squeeze", nil, []string{"main"}, []interface{}{s.snapshot}, s.factory, false)
		if t.errorExpected != "" {
			c.Check(err, ErrorMatches, t.errorExpected)
		} else {
			c.Check(repo.Prefix, Equals, t.expected)
		}
	}
}

func (s *PublishedRepoSuite) TestDistributionComponentGuessing(c *C) {
	repo, err := NewPublishedRepo("", "ppa", "", nil, []string{""}, []interface{}{s.snapshot}, s.factory, false)
	c.Check(err, IsNil)
	c.Check(repo.Distribution, Equals, "squeeze")
	c.Check(repo.Components(), DeepEquals, []string{"main"})

	repo, err = NewPublishedRepo("", "ppa", "wheezy", nil, []string{""}, []interface{}{s.snapshot}, s.factory, false)
	c.Check(err, IsNil)
	c.Check(repo.Distribution, Equals, "wheezy")
	c.Check(repo.Components(), DeepEquals, []string{"main"})

	repo, err = NewPublishedRepo("", "ppa", "", nil, []string{"non-free"}, []interface{}{s.snapshot}, s.factory, false)
	c.Check(err, IsNil)
	c.Check(repo.Distribution, Equals, "squeeze")
	c.Check(repo.Components(), DeepEquals, []string{"non-free"})

	repo, err = NewPublishedRepo("", "ppa", "squeeze", nil, []string{""}, []interface{}{s.localRepo}, s.factory, false)
	c.Check(err, IsNil)
	c.Check(repo.Distribution, Equals, "squeeze")
	c.Check(repo.Components(), DeepEquals, []string{"main"})

	_, err = NewPublishedRepo("", "ppa", "", nil, []string{"main"}, []interface{}{s.localRepo}, s.factory, false)
	c.Check(err, ErrorMatches, "unable to guess distribution name, please specify explicitly")

	s.localRepo.DefaultDistribution = "precise"
	s.localRepo.DefaultComponent = "contrib"
	_ = s.factory.LocalRepoCollection().Update(s.localRepo)

	repo, err = NewPublishedRepo("", "ppa", "", nil, []string{""}, []interface{}{s.localRepo}, s.factory, false)
	c.Check(err, IsNil)
	c.Check(repo.Distribution, Equals, "precise")
	c.Check(repo.Components(), DeepEquals, []string{"contrib"})

	s.localRepo.DefaultDistribution = "precise/updates"

	repo, err = NewPublishedRepo("", "ppa", "", nil, []string{""}, []interface{}{s.localRepo}, s.factory, false)
	c.Check(err, IsNil)
	c.Check(repo.Distribution, Equals, "precise/updates")
	c.Check(repo.Components(), DeepEquals, []string{"contrib"})

	repo, err = NewPublishedRepo("", "ppa", "", nil, []string{"", "contrib"}, []interface{}{s.snapshot, s.snapshot2}, s.factory, false)
	c.Check(err, IsNil)
	c.Check(repo.Distribution, Equals, "squeeze")
	c.Check(repo.Components(), DeepEquals, []string{"contrib", "main"})

	_, err = NewPublishedRepo("", "ppa", "", nil, []string{"", ""}, []interface{}{s.snapshot, s.snapshot2}, s.factory, false)
	c.Check(err, ErrorMatches, "duplicate component name: main")
}

func (s *PublishedRepoSuite) TestUpdate(c *C) {
	revision := s.repo2.ObtainRevision()
	sources := revision.Sources
	sources["test"] = "local1"

	result, err := s.repo2.Update(s.factory, nil)
	c.Assert(err, IsNil)
	c.Assert(result, NotNil)
	c.Assert(s.repo2.Revision, IsNil)

	c.Assert(result.AddedSources, DeepEquals, map[string]string{"test": "local1"})
	c.Assert(result.UpdatedSources, DeepEquals, map[string]string{"main": "local1"})
	c.Assert(result.RemovedSources, DeepEquals, map[string]string{})

	c.Assert(result.AddedComponents(), DeepEquals, []string{"test"})
	c.Assert(result.UpdatedComponents(), DeepEquals, []string{"main"})
	c.Assert(result.RemovedComponents(), DeepEquals, []string{})
}

func (s *PublishedRepoSuite) TestPublish(c *C) {
	err := s.repo.Publish(s.packagePool, s.provider, s.factory, &NullSigner{}, nil, false, "")
	c.Assert(err, IsNil)

	c.Check(s.repo.Architectures, DeepEquals, []string{"i386"})

	rf, err := os.Open(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/squeeze/Release"))
	c.Assert(err, IsNil)

	cfr := NewControlFileReader(rf, true, false)
	st, err := cfr.ReadStanza()
	c.Assert(err, IsNil)

	c.Check(st["Origin"], Equals, "ppa squeeze")
	c.Check(st["Components"], Equals, "main")
	c.Check(st["Architectures"], Equals, "i386")

	pf, err := os.Open(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/squeeze/main/binary-i386/Packages"))
	c.Assert(err, IsNil)

	cfr = NewControlFileReader(pf, false, false)

	for i := 0; i < 3; i++ {
		st, err = cfr.ReadStanza()
		c.Assert(err, IsNil)

		c.Check(st["Filename"], Equals, "pool/main/a/alien-arena/alien-arena-common_7.40-2_i386.deb")
	}

	st, err = cfr.ReadStanza()
	c.Assert(err, IsNil)
	c.Assert(st, IsNil)

	drf, err := os.Open(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/squeeze/main/binary-i386/Release"))
	c.Assert(err, IsNil)

	cfr = NewControlFileReader(drf, true, false)
	st, err = cfr.ReadStanza()
	c.Assert(err, IsNil)

	c.Check(st["Archive"], Equals, "squeeze")
	c.Check(st["Architecture"], Equals, "i386")

	_, err = os.Stat(filepath.Join(s.publishedStorage.PublicPath(), "ppa/pool/main/a/alien-arena/alien-arena-common_7.40-2_i386.deb"))
	c.Assert(err, IsNil)
}

func (s *PublishedRepoSuite) TestPublishNoSigner(c *C) {
	err := s.repo.Publish(s.packagePool, s.provider, s.factory, nil, nil, false, "")
	c.Assert(err, IsNil)

	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/squeeze/Release"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/squeeze/main/binary-i386/Release"), PathExists)
}

func (s *PublishedRepoSuite) TestPublishLocalRepo(c *C) {
	err := s.repo2.Publish(s.packagePool, s.provider, s.factory, nil, nil, false, "")
	c.Assert(err, IsNil)

	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/maverick/Release"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/maverick/main/binary-i386/Release"), PathExists)
}

func (s *PublishedRepoSuite) TestPublishLocalSourceRepo(c *C) {
	err := s.repo4.Publish(s.packagePool, s.provider, s.factory, nil, nil, false, "")
	c.Assert(err, IsNil)

	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/maverick/Release"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/maverick/main/source/Release"), PathExists)
}

func (s *PublishedRepoSuite) TestPublishOtherStorage(c *C) {
	err := s.repo5.Publish(s.packagePool, s.provider, s.factory, nil, nil, false, "")
	c.Assert(err, IsNil)

	c.Check(filepath.Join(s.publishedStorage2.PublicPath(), "ppa/dists/maverick/Release"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/maverick/Release"), Not(PathExists))
}

func (s *PublishedRepoSuite) TestString(c *C) {
	c.Check(s.repo.String(), Equals,
		"ppa/squeeze [] publishes {main: [snap]: Snapshot from mirror [yandex]: http://mirror.yandex.ru/debian/ squeeze}")
	c.Check(s.repo2.String(), Equals,
		"ppa/maverick [] publishes {main: [local1]: comment1}")
	repo, _ := NewPublishedRepo("", "", "squeeze", []string{"s390"}, []string{"main"}, []interface{}{s.snapshot}, s.factory, false)
	c.Check(repo.String(), Equals,
		"./squeeze [s390] publishes {main: [snap]: Snapshot from mirror [yandex]: http://mirror.yandex.ru/debian/ squeeze}")
	repo, _ = NewPublishedRepo("", "", "squeeze", []string{"i386", "amd64"}, []string{"main"}, []interface{}{s.snapshot}, s.factory, false)
	c.Check(repo.String(), Equals,
		"./squeeze [i386, amd64] publishes {main: [snap]: Snapshot from mirror [yandex]: http://mirror.yandex.ru/debian/ squeeze}")
	repo.Origin = "myorigin"
	c.Check(repo.String(), Equals,
		"./squeeze (origin: myorigin) [i386, amd64] publishes {main: [snap]: Snapshot from mirror [yandex]: http://mirror.yandex.ru/debian/ squeeze}")
	repo.Label = "mylabel"
	c.Check(repo.String(), Equals,
		"./squeeze (origin: myorigin, label: mylabel) [i386, amd64] publishes {main: [snap]: Snapshot from mirror [yandex]: http://mirror.yandex.ru/debian/ squeeze}")
	c.Check(s.repo3.String(), Equals,
		"linux/natty [] publishes {contrib: [snap]: Snapshot from mirror [yandex]: http://mirror.yandex.ru/debian/ squeeze}, {main: [snap]: Snapshot from mirror [yandex]: http://mirror.yandex.ru/debian/ squeeze}")
	c.Check(s.repo5.String(), Equals,
		"files:other:ppa/maverick [source] publishes {main: [local1]: comment1}")
}

func (s *PublishedRepoSuite) TestKey(c *C) {
	c.Check(s.repo.Key(), DeepEquals, []byte("Uppa>>squeeze"))
	c.Check(s.repo5.Key(), DeepEquals, []byte("Ufiles:other:ppa>>maverick"))
}

func (s *PublishedRepoSuite) TestRefKey(c *C) {
	c.Check(s.repo.RefKey(""), DeepEquals, []byte("E"+s.repo.UUID))
	c.Check(s.repo.RefKey("main"), DeepEquals, []byte("E"+s.repo.UUID+"main"))
}

func (s *PublishedRepoSuite) TestEncodeDecode(c *C) {
	encoded := s.repo.Encode()
	repo := &PublishedRepo{}
	err := repo.Decode(encoded)

	s.repo.sourceItems = nil
	c.Assert(err, IsNil)
	c.Assert(repo, DeepEquals, s.repo)

	encoded2 := s.repo2.Encode()
	repo2 := &PublishedRepo{}
	err = repo2.Decode(encoded2)

	s.repo2.sourceItems = nil
	c.Assert(err, IsNil)
	c.Assert(repo2, DeepEquals, s.repo2)
}

func (s *PublishedRepoSuite) TestPublishedRepoRevision(c *C) {
	revision := s.repo2.ObtainRevision()
	c.Assert(revision, NotNil)

	sources := revision.Sources
	c.Assert(sources, NotNil)
	c.Assert(sources, DeepEquals, map[string]string{"main": "local1"})

	sources["test1"] = "snap1"
	sources["test2"] = "snap2"

	c.Assert(revision.Components(), DeepEquals, []string{"main", "test1", "test2"})
	c.Assert(revision.SourceNames(), DeepEquals, []string{"local1", "snap1", "snap2"})

	bytes, err := json.Marshal(revision)
	c.Assert(err, IsNil)

	jsonExpected := `{"Sources":[{"Component":"main","Name":"local1"},{"Component":"test1","Name":"snap1"},{"Component":"test2","Name":"snap2"}]}`
	c.Assert(string(bytes), Equals, jsonExpected)

	c.Assert(s.repo2.DropRevision(), DeepEquals, revision)
	c.Assert(s.repo2.Revision, IsNil)
}

type PublishedRepoCollectionSuite struct {
	PackageListMixinSuite
	db                                database.Storage
	factory                           *CollectionFactory
	snapshotCollection                *SnapshotCollection
	collection                        *PublishedRepoCollection
	snap1, snap2                      *Snapshot
	localRepo                         *LocalRepo
	repo1, repo2, repo3, repo4, repo5 *PublishedRepo
}

var _ = Suite(&PublishedRepoCollectionSuite{})

func (s *PublishedRepoCollectionSuite) SetUpTest(c *C) {
	s.SetUpPackages()

	s.db, _ = goleveldb.NewOpenDB(c.MkDir())
	s.factory = NewCollectionFactory(s.db)

	s.snapshotCollection = s.factory.SnapshotCollection()

	snap1Refs := NewPackageRefList()
	snap1Refs.Refs = [][]byte{s.p1.Key(""), s.p2.Key("")}
	sort.Sort(snap1Refs)
	s.snap1 = NewSnapshotFromRefList("snap1", []*Snapshot{}, snap1Refs, "desc1")

	snap2Refs := NewPackageRefList()
	snap2Refs.Refs = [][]byte{s.p3.Key("")}
	sort.Sort(snap2Refs)
	s.snap2 = NewSnapshotFromRefList("snap2", []*Snapshot{}, snap2Refs, "desc2")

	_ = s.snapshotCollection.Add(s.snap1)
	_ = s.snapshotCollection.Add(s.snap2)

	s.localRepo = NewLocalRepo("local1", "comment1")
	_ = s.factory.LocalRepoCollection().Add(s.localRepo)

	s.repo1, _ = NewPublishedRepo("", "ppa", "anaconda", []string{}, []string{"main"}, []interface{}{s.snap1}, s.factory, false)
	s.repo2, _ = NewPublishedRepo("", "", "anaconda", []string{}, []string{"main", "contrib"}, []interface{}{s.snap2, s.snap1}, s.factory, false)
	s.repo3, _ = NewPublishedRepo("", "ppa", "anaconda", []string{}, []string{"main"}, []interface{}{s.snap2}, s.factory, false)
	s.repo4, _ = NewPublishedRepo("", "ppa", "precise", []string{}, []string{"main"}, []interface{}{s.localRepo}, s.factory, false)
	s.repo5, _ = NewPublishedRepo("files:other", "ppa", "precise", []string{}, []string{"main"}, []interface{}{s.localRepo}, s.factory, false)

	s.collection = s.factory.PublishedRepoCollection()
}

func (s *PublishedRepoCollectionSuite) TearDownTest(c *C) {
	_ = s.db.Close()
}

func (s *PublishedRepoCollectionSuite) TestAddByStoragePrefixDistribution(c *C) {
	_, err := s.collection.ByStoragePrefixDistribution("", "ppa", "anaconda")
	c.Assert(err, ErrorMatches, "*.not found")

	c.Assert(s.collection.Add(s.repo1), IsNil)
	c.Assert(s.collection.Add(s.repo1), ErrorMatches, ".*already exists")
	c.Assert(s.collection.CheckDuplicate(s.repo2), IsNil)
	c.Assert(s.collection.Add(s.repo2), IsNil)
	c.Assert(s.collection.Add(s.repo3), ErrorMatches, ".*already exists")
	c.Assert(s.collection.CheckDuplicate(s.repo3), Equals, s.repo1)
	c.Assert(s.collection.Add(s.repo4), IsNil)
	c.Assert(s.collection.Add(s.repo5), IsNil)

	r, err := s.collection.ByStoragePrefixDistribution("", "ppa", "anaconda")
	c.Assert(err, IsNil)

	err = s.collection.LoadComplete(r, s.factory)
	c.Assert(err, IsNil)
	c.Assert(r.String(), Equals, s.repo1.String())

	collection := NewPublishedRepoCollection(s.db)
	r, err = collection.ByStoragePrefixDistribution("", "ppa", "anaconda")
	c.Assert(err, IsNil)

	err = s.collection.LoadComplete(r, s.factory)
	c.Assert(err, IsNil)
	c.Assert(r.String(), Equals, s.repo1.String())

	r, err = s.collection.ByStoragePrefixDistribution("files:other", "ppa", "precise")
	c.Assert(err, IsNil)
	c.Check(r.String(), Equals, s.repo5.String())
}

func (s *PublishedRepoCollectionSuite) TestByUUID(c *C) {
	_, err := s.collection.ByUUID(s.repo1.UUID)
	c.Assert(err, ErrorMatches, "*.not found")

	c.Assert(s.collection.Add(s.repo1), IsNil)

	r, err := s.collection.ByUUID(s.repo1.UUID)
	c.Assert(err, IsNil)

	err = s.collection.LoadComplete(r, s.factory)
	c.Assert(err, IsNil)
	c.Assert(r.String(), Equals, s.repo1.String())
}

func (s *PublishedRepoCollectionSuite) TestUpdateLoadComplete(c *C) {
	c.Assert(s.collection.Update(s.repo1), IsNil)
	c.Assert(s.collection.Update(s.repo4), IsNil)

	collection := NewPublishedRepoCollection(s.db)
	r, err := collection.ByStoragePrefixDistribution("", "ppa", "anaconda")
	c.Assert(err, IsNil)
	c.Assert(r.sourceItems["main"].snapshot, IsNil)
	c.Assert(s.collection.LoadComplete(r, s.factory), IsNil)
	c.Assert(r.Sources["main"], Equals, s.repo1.sourceItems["main"].snapshot.UUID)
	c.Assert(r.RefList("main").Len(), Equals, 2)

	r, err = collection.ByStoragePrefixDistribution("", "ppa", "precise")
	c.Assert(err, IsNil)
	c.Assert(r.sourceItems["main"].localRepo, IsNil)
	c.Assert(s.collection.LoadComplete(r, s.factory), IsNil)
	c.Assert(r.sourceItems["main"].localRepo.UUID, Equals, s.repo4.sourceItems["main"].localRepo.UUID)
	c.Assert(r.sourceItems["main"].packageRefs.Len(), Equals, 0)
	c.Assert(r.RefList("main").Len(), Equals, 0)
}

func (s *PublishedRepoCollectionSuite) TestLoadPre0_6(c *C) {
	type oldPublishedRepo struct {
		UUID          string
		Prefix        string
		Distribution  string
		Origin        string
		Label         string
		Architectures []string
		SourceKind    string
		Component     string
		SourceUUID    string `codec:"SnapshotUUID"`
	}

	old := oldPublishedRepo{
		UUID:          s.repo1.UUID,
		Prefix:        "ppa",
		Distribution:  "anaconda",
		Architectures: []string{"i386"},
		SourceKind:    SourceLocalRepo,
		Component:     "contrib",
		SourceUUID:    s.localRepo.UUID,
	}

	var buf bytes.Buffer

	encoder := codec.NewEncoder(&buf, &codec.MsgpackHandle{})
	_ = encoder.Encode(&old)

	c.Assert(s.db.Put(s.repo1.Key(), buf.Bytes()), IsNil)
	c.Assert(s.db.Put(s.repo1.RefKey(""), s.localRepo.RefList().Encode()), IsNil)

	collection := NewPublishedRepoCollection(s.db)
	repo, err := collection.ByStoragePrefixDistribution("", "ppa", "anaconda")
	c.Check(err, IsNil)
	c.Check(repo.Component, Equals, "")
	c.Check(repo.SourceUUID, Equals, "")
	c.Check(repo.Sources, DeepEquals, map[string]string{"contrib": s.localRepo.UUID})

	c.Check(collection.LoadComplete(repo, s.factory), IsNil)
	c.Check(repo.sourceItems["contrib"].localRepo.UUID, Equals, s.localRepo.UUID)
	c.Check(repo.RefList("contrib").Len(), Equals, 0)
}

func (s *PublishedRepoCollectionSuite) TestForEachAndLen(c *C) {
	_ = s.collection.Add(s.repo1)

	count := 0
	err := s.collection.ForEach(func(*PublishedRepo) error {
		count++
		return nil
	})
	c.Assert(count, Equals, 1)
	c.Assert(err, IsNil)

	c.Check(s.collection.Len(), Equals, 1)

	e := errors.New("c")

	err = s.collection.ForEach(func(*PublishedRepo) error {
		return e
	})
	c.Assert(err, Equals, e)
}

func (s *PublishedRepoCollectionSuite) TestBySnapshot(c *C) {
	c.Check(s.collection.Add(s.repo1), IsNil)
	c.Check(s.collection.Add(s.repo2), IsNil)

	c.Check(s.collection.BySnapshot(s.snap1), DeepEquals, []*PublishedRepo{s.repo1, s.repo2})
	c.Check(s.collection.BySnapshot(s.snap2), DeepEquals, []*PublishedRepo{s.repo2})
}

func (s *PublishedRepoCollectionSuite) TestByLocalRepo(c *C) {
	c.Check(s.collection.Add(s.repo1), IsNil)
	c.Check(s.collection.Add(s.repo4), IsNil)
	c.Check(s.collection.Add(s.repo5), IsNil)

	c.Check(s.collection.ByLocalRepo(s.localRepo), DeepEquals, []*PublishedRepo{s.repo4, s.repo5})
}

func (s *PublishedRepoCollectionSuite) TestListReferencedFiles(c *C) {
	c.Check(s.factory.PackageCollection().Update(s.p1), IsNil)
	c.Check(s.factory.PackageCollection().Update(s.p2), IsNil)
	c.Check(s.factory.PackageCollection().Update(s.p3), IsNil)

	c.Check(s.collection.Add(s.repo1), IsNil)
	c.Check(s.collection.Add(s.repo2), IsNil)
	c.Check(s.collection.Add(s.repo4), IsNil)
	c.Check(s.collection.Add(s.repo5), IsNil)

	files, err := s.collection.listReferencedFilesByComponent(".", []string{"main", "contrib"}, s.factory, nil)
	c.Assert(err, IsNil)
	for _, v := range files {
		sort.Strings(v)
	}
	c.Check(files, DeepEquals, map[string][]string{
		"contrib": {
			"a/alien-arena/alien-arena-common_7.40-2_i386.deb",
			"a/alien-arena/mars-invaders_7.40-2_i386.deb",
		},
		"main": {"a/alien-arena/lonely-strangers_7.40-2_i386.deb"},
	})

	snap3 := NewSnapshotFromRefList("snap3", []*Snapshot{}, s.snap2.RefList(), "desc3")
	_ = s.snapshotCollection.Add(snap3)

	// Ensure that adding a second publish point with matching files doesn't give duplicate results.
	repo3, err := NewPublishedRepo("", "", "anaconda-2", []string{}, []string{"main"}, []interface{}{snap3}, s.factory, false)
	c.Check(err, IsNil)
	c.Check(s.collection.Add(repo3), IsNil)

	files, err = s.collection.listReferencedFilesByComponent(".", []string{"main", "contrib"}, s.factory, nil)
	c.Assert(err, IsNil)
	for _, v := range files {
		sort.Strings(v)
	}
	c.Check(files, DeepEquals, map[string][]string{
		"contrib": {
			"a/alien-arena/alien-arena-common_7.40-2_i386.deb",
			"a/alien-arena/mars-invaders_7.40-2_i386.deb",
		},
		"main": {"a/alien-arena/lonely-strangers_7.40-2_i386.deb"},
	})
}

type PublishedRepoRemoveSuite struct {
	PackageListMixinSuite
	db                                  database.Storage
	factory                             *CollectionFactory
	snapshotCollection                  *SnapshotCollection
	collection                          *PublishedRepoCollection
	root, root2                         string
	provider                            *FakeStorageProvider
	publishedStorage, publishedStorage2 *files.PublishedStorage
	snap1                               *Snapshot
	repo1, repo2, repo3, repo4, repo5   *PublishedRepo
}

var _ = Suite(&PublishedRepoRemoveSuite{})

func (s *PublishedRepoRemoveSuite) SetUpTest(c *C) {
	s.db, _ = goleveldb.NewOpenDB(c.MkDir())
	s.factory = NewCollectionFactory(s.db)

	s.snapshotCollection = s.factory.SnapshotCollection()

	s.snap1 = NewSnapshotFromPackageList("snap1", []*Snapshot{}, NewPackageList(), "desc1")

	_ = s.snapshotCollection.Add(s.snap1)

	s.repo1, _ = NewPublishedRepo("", "ppa", "anaconda", []string{}, []string{"main"}, []interface{}{s.snap1}, s.factory, false)
	s.repo2, _ = NewPublishedRepo("", "", "anaconda", []string{}, []string{"main"}, []interface{}{s.snap1}, s.factory, false)
	s.repo3, _ = NewPublishedRepo("", "ppa", "meduza", []string{}, []string{"main"}, []interface{}{s.snap1}, s.factory, false)
	s.repo4, _ = NewPublishedRepo("", "ppa", "osminog", []string{}, []string{"contrib"}, []interface{}{s.snap1}, s.factory, false)
	s.repo5, _ = NewPublishedRepo("files:other", "ppa", "osminog", []string{}, []string{"contrib"}, []interface{}{s.snap1}, s.factory, false)

	s.collection = s.factory.PublishedRepoCollection()
	_ = s.collection.Add(s.repo1)
	_ = s.collection.Add(s.repo2)
	_ = s.collection.Add(s.repo3)
	_ = s.collection.Add(s.repo4)
	_ = s.collection.Add(s.repo5)

	s.root = c.MkDir()
	s.publishedStorage = files.NewPublishedStorage(s.root, "", "")
	_ = s.publishedStorage.MkDir("ppa/dists/anaconda")
	_ = s.publishedStorage.MkDir("ppa/dists/meduza")
	_ = s.publishedStorage.MkDir("ppa/dists/osminog")
	_ = s.publishedStorage.MkDir("ppa/pool/main")
	_ = s.publishedStorage.MkDir("ppa/pool/contrib")
	_ = s.publishedStorage.MkDir("dists/anaconda")
	_ = s.publishedStorage.MkDir("pool/main")

	s.root2 = c.MkDir()
	s.publishedStorage2 = files.NewPublishedStorage(s.root2, "", "")
	_ = s.publishedStorage2.MkDir("ppa/dists/osminog")
	_ = s.publishedStorage2.MkDir("ppa/pool/contrib")

	s.provider = &FakeStorageProvider{map[string]aptly.PublishedStorage{
		"":            s.publishedStorage,
		"files:other": s.publishedStorage2}}
}

func (s *PublishedRepoRemoveSuite) TearDownTest(c *C) {
	_ = s.db.Close()
}

func (s *PublishedRepoRemoveSuite) TestRemoveFilesOnlyDist(c *C) {
	_ = s.repo1.RemoveFiles(s.provider, false, []string{}, nil)

	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/anaconda"), Not(PathExists))
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/meduza"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/osminog"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/pool/main"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/pool/contrib"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "dists/anaconda"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "pool/main"), PathExists)
	c.Check(filepath.Join(s.publishedStorage2.PublicPath(), "ppa/dists/osminog"), PathExists)
	c.Check(filepath.Join(s.publishedStorage2.PublicPath(), "ppa/pool/contrib"), PathExists)
}

func (s *PublishedRepoRemoveSuite) TestRemoveFilesWithPool(c *C) {
	_ = s.repo1.RemoveFiles(s.provider, false, []string{"main"}, nil)

	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/anaconda"), Not(PathExists))
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/meduza"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/osminog"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/pool/main"), Not(PathExists))
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/pool/contrib"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "dists/anaconda"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "pool/main"), PathExists)
	c.Check(filepath.Join(s.publishedStorage2.PublicPath(), "ppa/dists/osminog"), PathExists)
	c.Check(filepath.Join(s.publishedStorage2.PublicPath(), "ppa/pool/contrib"), PathExists)
}

func (s *PublishedRepoRemoveSuite) TestRemoveFilesWithTwoPools(c *C) {
	_ = s.repo1.RemoveFiles(s.provider, false, []string{"main", "contrib"}, nil)

	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/anaconda"), Not(PathExists))
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/meduza"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/osminog"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/pool/main"), Not(PathExists))
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/pool/contrib"), Not(PathExists))
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "dists/anaconda"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "pool/main"), PathExists)
	c.Check(filepath.Join(s.publishedStorage2.PublicPath(), "ppa/dists/osminog"), PathExists)
	c.Check(filepath.Join(s.publishedStorage2.PublicPath(), "ppa/pool/contrib"), PathExists)
}

func (s *PublishedRepoRemoveSuite) TestRemoveFilesWithPrefix(c *C) {
	_ = s.repo1.RemoveFiles(s.provider, true, []string{"main"}, nil)

	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/anaconda"), Not(PathExists))
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/meduza"), Not(PathExists))
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/osminog"), Not(PathExists))
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/pool/main"), Not(PathExists))
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/pool/contrib"), Not(PathExists))
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "dists/anaconda"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "pool/main"), PathExists)
	c.Check(filepath.Join(s.publishedStorage2.PublicPath(), "ppa/dists/osminog"), PathExists)
	c.Check(filepath.Join(s.publishedStorage2.PublicPath(), "ppa/pool/contrib"), PathExists)
}

func (s *PublishedRepoRemoveSuite) TestRemoveFilesWithPrefixRoot(c *C) {
	_ = s.repo2.RemoveFiles(s.provider, true, []string{"main"}, nil)

	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/anaconda"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/meduza"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/pool/main"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/pool/contrib"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "dists/anaconda"), Not(PathExists))
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "pool/main"), Not(PathExists))
	c.Check(filepath.Join(s.publishedStorage2.PublicPath(), "ppa/dists/osminog"), PathExists)
	c.Check(filepath.Join(s.publishedStorage2.PublicPath(), "ppa/pool/contrib"), PathExists)
}

func (s *PublishedRepoRemoveSuite) TestRemoveRepo1and2(c *C) {
	err := s.collection.Remove(s.provider, "", "ppa", "anaconda", s.factory, nil, false, false)
	c.Check(err, IsNil)

	_, err = s.collection.ByStoragePrefixDistribution("", "ppa", "anaconda")
	c.Check(err, ErrorMatches, ".*not found")

	collection := NewPublishedRepoCollection(s.db)
	_, err = collection.ByStoragePrefixDistribution("", "ppa", "anaconda")
	c.Check(err, ErrorMatches, ".*not found")

	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/anaconda"), Not(PathExists))
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/meduza"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/osminog"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/pool/main"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/pool/contrib"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "dists/anaconda"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "pool/main"), PathExists)
	c.Check(filepath.Join(s.publishedStorage2.PublicPath(), "ppa/dists/osminog"), PathExists)
	c.Check(filepath.Join(s.publishedStorage2.PublicPath(), "ppa/pool/contrib"), PathExists)

	err = s.collection.Remove(s.provider, "", "ppa", "anaconda", s.factory, nil, false, false)
	c.Check(err, ErrorMatches, ".*not found")

	err = s.collection.Remove(s.provider, "", "ppa", "meduza", s.factory, nil, false, false)
	c.Check(err, IsNil)

	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/anaconda"), Not(PathExists))
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/meduza"), Not(PathExists))
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/osminog"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/pool/main"), Not(PathExists))
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/pool/contrib"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "dists/anaconda"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "pool/main"), PathExists)
	c.Check(filepath.Join(s.publishedStorage2.PublicPath(), "ppa/dists/osminog"), PathExists)
	c.Check(filepath.Join(s.publishedStorage2.PublicPath(), "ppa/pool/contrib"), PathExists)
}

func (s *PublishedRepoRemoveSuite) TestRemoveRepo1and2SkipCleanup(c *C) {
	err := s.collection.Remove(s.provider, "", "ppa", "anaconda", s.factory, nil, false, true)
	c.Check(err, IsNil)

	_, err = s.collection.ByStoragePrefixDistribution("", "ppa", "anaconda")
	c.Check(err, ErrorMatches, ".*not found")

	collection := NewPublishedRepoCollection(s.db)
	_, err = collection.ByStoragePrefixDistribution("", "ppa", "anaconda")
	c.Check(err, ErrorMatches, ".*not found")

	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/anaconda"), Not(PathExists))
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/meduza"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/osminog"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/pool/main"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/pool/contrib"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "dists/anaconda"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "pool/main"), PathExists)
	c.Check(filepath.Join(s.publishedStorage2.PublicPath(), "ppa/dists/osminog"), PathExists)
	c.Check(filepath.Join(s.publishedStorage2.PublicPath(), "ppa/pool/contrib"), PathExists)

	err = s.collection.Remove(s.provider, "", "ppa", "anaconda", s.factory, nil, false, true)
	c.Check(err, ErrorMatches, ".*not found")

	err = s.collection.Remove(s.provider, "", "ppa", "meduza", s.factory, nil, false, true)
	c.Check(err, IsNil)

	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/anaconda"), Not(PathExists))
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/meduza"), Not(PathExists))
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/osminog"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/pool/main"), Not(PathExists))
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/pool/contrib"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "dists/anaconda"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "pool/main"), PathExists)
	c.Check(filepath.Join(s.publishedStorage2.PublicPath(), "ppa/dists/osminog"), PathExists)
	c.Check(filepath.Join(s.publishedStorage2.PublicPath(), "ppa/pool/contrib"), PathExists)
}

func (s *PublishedRepoRemoveSuite) TestRemoveRepo3(c *C) {
	err := s.collection.Remove(s.provider, "", ".", "anaconda", s.factory, nil, false, false)
	c.Check(err, IsNil)

	_, err = s.collection.ByStoragePrefixDistribution("", ".", "anaconda")
	c.Check(err, ErrorMatches, ".*not found")

	collection := NewPublishedRepoCollection(s.db)
	_, err = collection.ByStoragePrefixDistribution("", ".", "anaconda")
	c.Check(err, ErrorMatches, ".*not found")

	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/anaconda"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/meduza"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/osminog"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/pool/main"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/pool/contrib"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "dists/"), Not(PathExists))
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "pool/"), Not(PathExists))
	c.Check(filepath.Join(s.publishedStorage2.PublicPath(), "ppa/dists/osminog"), PathExists)
	c.Check(filepath.Join(s.publishedStorage2.PublicPath(), "ppa/pool/contrib"), PathExists)
}

func (s *PublishedRepoRemoveSuite) TestRemoveRepo5(c *C) {
	err := s.collection.Remove(s.provider, "files:other", "ppa", "osminog", s.factory, nil, false, false)
	c.Check(err, IsNil)

	_, err = s.collection.ByStoragePrefixDistribution("files:other", "ppa", "osminog")
	c.Check(err, ErrorMatches, ".*not found")

	collection := NewPublishedRepoCollection(s.db)
	_, err = collection.ByStoragePrefixDistribution("files:other", "ppa", "osminog")
	c.Check(err, ErrorMatches, ".*not found")

	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/anaconda"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/meduza"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/dists/osminog"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/pool/main"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "ppa/pool/contrib"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "dists/"), PathExists)
	c.Check(filepath.Join(s.publishedStorage.PublicPath(), "pool/"), PathExists)
	c.Check(filepath.Join(s.publishedStorage2.PublicPath(), "ppa/dists/osminog"), Not(PathExists))
	c.Check(filepath.Join(s.publishedStorage2.PublicPath(), "ppa/pool/contrib"), Not(PathExists))
}
