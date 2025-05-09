package swift

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ncw/swift/swifttest"

	"github.com/aptly-dev/aptly/files"
	"github.com/aptly-dev/aptly/utils"
)

type PublishedStorageSuite struct {
	TestAddress, AuthURL     string
	srv                      *swifttest.SwiftServer
	storage, prefixedStorage *PublishedStorage
}

var _ = Suite(&PublishedStorageSuite{})

func (s *PublishedStorageSuite) SetUpTest(c *C) {
	var err error

	s.TestAddress = fmt.Sprintf("localhost:%d", rand.Intn(10000)+20000)
	s.AuthURL = "http://" + s.TestAddress + "/v1.0"

	s.srv, err = swifttest.NewSwiftServer(s.TestAddress)
	c.Assert(err, IsNil)
	c.Assert(s.srv, NotNil)

	s.storage, err = NewPublishedStorage("swifttest", "swifttest", s.AuthURL, "", "", "", "", "", "", "test", "")
	c.Assert(err, IsNil)

	s.prefixedStorage, err = NewPublishedStorage("swifttest", "swifttest", s.AuthURL, "", "", "", "", "", "", "test", "lala")
	c.Assert(err, IsNil)

	_ = s.storage.conn.ContainerCreate("test", nil)
}

func (s *PublishedStorageSuite) TearDownTest(c *C) {
	s.srv.Close()
}

func (s *PublishedStorageSuite) TestNewPublishedStorage(c *C) {
	stor, err := NewPublishedStorage("swifttest", "swifttest", s.AuthURL, "", "", "", "", "", "", "", "")
	c.Check(stor, NotNil)
	c.Check(err, IsNil)
}

func (s *PublishedStorageSuite) TestPutFile(c *C) {
	dir := c.MkDir()
	err := os.WriteFile(filepath.Join(dir, "a"), []byte("welcome to swift!"), 0644)
	c.Assert(err, IsNil)

	err = s.storage.PutFile("a/b.txt", filepath.Join(dir, "a"))
	c.Check(err, IsNil)

	data, err := s.storage.conn.ObjectGetBytes("test", "a/b.txt")
	c.Check(err, IsNil)
	c.Check(data, DeepEquals, []byte("welcome to swift!"))

	err = s.prefixedStorage.PutFile("a/b.txt", filepath.Join(dir, "a"))
	c.Check(err, IsNil)

	data, err = s.storage.conn.ObjectGetBytes("test", "lala/a/b.txt")
	c.Check(err, IsNil)
	c.Check(data, DeepEquals, []byte("welcome to swift!"))
}

func (s *PublishedStorageSuite) TestFilelist(c *C) {
	dir := c.MkDir()
	err := os.WriteFile(filepath.Join(dir, "a"), []byte("welcome to swift!"), 0644)
	c.Assert(err, IsNil)

	paths := []string{"a", "b", "c", "testa", "test/a", "test/b", "lala/a", "lala/b", "lala/c"}
	for _, path := range paths {
		err = s.storage.PutFile(path, filepath.Join(dir, "a"))
		c.Check(err, IsNil)
	}

	list, err := s.storage.Filelist("")
	c.Check(err, IsNil)
	c.Check(list, DeepEquals, []string{"a", "b", "c", "lala/a", "lala/b", "lala/c", "test/a", "test/b", "testa"})

	list, err = s.storage.Filelist("test")
	c.Check(err, IsNil)
	c.Check(list, DeepEquals, []string{"a", "b"})

	list, err = s.storage.Filelist("test2")
	c.Check(err, IsNil)
	c.Check(list, DeepEquals, []string{})

	list, err = s.prefixedStorage.Filelist("")
	c.Check(err, IsNil)
	c.Check(list, DeepEquals, []string{"a", "b", "c"})
}

func (s *PublishedStorageSuite) TestRemove(c *C) {
	dir := c.MkDir()
	err := os.WriteFile(filepath.Join(dir, "a"), []byte("welcome to swift!"), 0644)
	c.Assert(err, IsNil)

	err = s.storage.PutFile("a/b.txt", filepath.Join(dir, "a"))
	c.Check(err, IsNil)

	err = s.storage.Remove("a/b.txt")
	c.Check(err, IsNil)

	_, err = s.storage.conn.ObjectGetBytes("test", "a/b.txt")
	c.Check(err, ErrorMatches, "Object Not Found")
}

func (s *PublishedStorageSuite) TestRemoveDirs(c *C) {
	c.Skip("bulk-delete not available in s3test")

	dir := c.MkDir()
	err := os.WriteFile(filepath.Join(dir, "a"), []byte("welcome to swift!"), 0644)
	c.Assert(err, IsNil)

	paths := []string{"a", "b", "c", "testa", "test/a", "test/b", "lala/a", "lala/b", "lala/c"}
	for _, path := range paths {
		err = s.storage.PutFile(path, filepath.Join(dir, "a"))
		c.Check(err, IsNil)
	}

	err = s.storage.RemoveDirs("test", nil)
	c.Check(err, IsNil)

	list, err := s.storage.Filelist("")
	c.Check(err, IsNil)
	c.Check(list, DeepEquals, []string{"a", "b", "c", "lala/a", "lala/b", "lala/c", "test/a", "test/b", "testa"})
}

func (s *PublishedStorageSuite) TestRenameFile(c *C) {
	c.Skip("copy not available in s3test")
}

func (s *PublishedStorageSuite) TestLinkFromPool(c *C) {
	root := c.MkDir()
	pool := files.NewPackagePool(root, false)
	cs := files.NewMockChecksumStorage()

	tmpFile1 := filepath.Join(c.MkDir(), "mars-invaders_1.03.deb")
	err := os.WriteFile(tmpFile1, []byte("Contents"), 0644)
	c.Assert(err, IsNil)
	cksum1 := utils.ChecksumInfo{MD5: "c1df1da7a1ce305a3b60af9d5733ac1d"}

	tmpFile2 := filepath.Join(c.MkDir(), "mars-invaders_1.03.deb")
	err = os.WriteFile(tmpFile2, []byte("Spam"), 0644)
	c.Assert(err, IsNil)
	cksum2 := utils.ChecksumInfo{MD5: "e9dfd31cc505d51fc26975250750deab"}

	tmpFile3 := filepath.Join(c.MkDir(), "netboot/boot.img.gz")
	_ = os.MkdirAll(filepath.Dir(tmpFile3), 0777)
	err = os.WriteFile(tmpFile3, []byte("Contents"), 0644)
	c.Assert(err, IsNil)
	cksum3 := utils.ChecksumInfo{MD5: "c1df1da7a1ce305a3b60af9d5733ac1d"}

	src1, err := pool.Import(tmpFile1, "mars-invaders_1.03.deb", &cksum1, true, cs)
	c.Assert(err, IsNil)
	src2, err := pool.Import(tmpFile2, "mars-invaders_1.03.deb", &cksum2, true, cs)
	c.Assert(err, IsNil)
	src3, err := pool.Import(tmpFile3, "netboot/boot.img.gz", &cksum3, true, cs)
	c.Assert(err, IsNil)

	// first link from pool
	err = s.storage.LinkFromPool("", filepath.Join("pool", "main", "m/mars-invaders"), "mars-invaders_1.03.deb", pool, src1, cksum1, false)
	c.Check(err, IsNil)

	data, err := s.storage.conn.ObjectGetBytes("test", "pool/main/m/mars-invaders/mars-invaders_1.03.deb")
	c.Check(err, IsNil)
	c.Check(data, DeepEquals, []byte("Contents"))

	// duplicate link from pool
	err = s.storage.LinkFromPool("", filepath.Join("pool", "main", "m/mars-invaders"), "mars-invaders_1.03.deb", pool, src1, cksum1, false)
	c.Check(err, IsNil)

	data, err = s.storage.conn.ObjectGetBytes("test", "pool/main/m/mars-invaders/mars-invaders_1.03.deb")
	c.Check(err, IsNil)
	c.Check(data, DeepEquals, []byte("Contents"))

	// link from pool with conflict
	err = s.storage.LinkFromPool("", filepath.Join("pool", "main", "m/mars-invaders"), "mars-invaders_1.03.deb", pool, src2, cksum2, false)
	c.Check(err, ErrorMatches, ".*file already exists and is different.*")

	data, err = s.storage.conn.ObjectGetBytes("test", "pool/main/m/mars-invaders/mars-invaders_1.03.deb")
	c.Check(err, IsNil)
	c.Check(data, DeepEquals, []byte("Contents"))

	// link from pool with conflict and force
	err = s.storage.LinkFromPool("", filepath.Join("pool", "main", "m/mars-invaders"), "mars-invaders_1.03.deb", pool, src2, cksum2, true)
	c.Check(err, IsNil)

	data, err = s.storage.conn.ObjectGetBytes("test", "pool/main/m/mars-invaders/mars-invaders_1.03.deb")
	c.Check(err, IsNil)
	c.Check(data, DeepEquals, []byte("Spam"))

	// link from pool with nested file name
	err = s.storage.LinkFromPool("", "dists/jessie/non-free/installer-i386/current/images", "netboot/boot.img.gz", pool, src3, cksum3, false)
	c.Check(err, IsNil)

	data, err = s.storage.conn.ObjectGetBytes("test", "dists/jessie/non-free/installer-i386/current/images/netboot/boot.img.gz")
	c.Check(err, IsNil)
	c.Check(data, DeepEquals, []byte("Contents"))
}

func (s *PublishedStorageSuite) TestSymLink(c *C) {
	dir := c.MkDir()
	err := os.WriteFile(filepath.Join(dir, "a"), []byte("welcome to swift!"), 0644)
	c.Assert(err, IsNil)

	err = s.storage.PutFile("a/b.txt", filepath.Join(dir, "a"))
	c.Check(err, IsNil)

	err = s.storage.SymLink("a/b.txt", "a/b.txt.link")
	c.Check(err, IsNil)

	var link string
	link, err = s.storage.ReadLink("a/b.txt.link")
	c.Check(err, IsNil)
	c.Check(link, Equals, "a/b")

	c.Skip("copy not availbale in s3test")
}

func (s *PublishedStorageSuite) TestFileExists(c *C) {
	dir := c.MkDir()
	err := os.WriteFile(filepath.Join(dir, "a"), []byte("welcome to swift!"), 0644)
	c.Assert(err, IsNil)

	err = s.storage.PutFile("a/b.txt", filepath.Join(dir, "a"))
	c.Check(err, IsNil)

	exists, err := s.storage.FileExists("a/b.txt")
	c.Check(err, IsNil)
	c.Check(exists, Equals, true)

	exists, err = s.storage.FileExists("a/b.invalid")
	c.Check(err, IsNil)
	c.Check(exists, Equals, false)
}
