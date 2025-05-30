package azure

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"io"
	"os"
	"path/filepath"
        "bytes"

        "github.com/Azure/azure-sdk-for-go/sdk/azcore"
        "github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
        "github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/aptly-dev/aptly/files"
	"github.com/aptly-dev/aptly/utils"
	. "gopkg.in/check.v1"
)

type PublishedStorageSuite struct {
	accountName, accountKey, endpoint string
	storage, prefixedStorage          *PublishedStorage
}

var _ = Suite(&PublishedStorageSuite{})

const testContainerPrefix = "aptlytest-"

func randContainer() string {
	return testContainerPrefix + randString(32-len(testContainerPrefix))
}

func randString(n int) string {
	if n <= 0 {
		panic("negative number")
	}
	const alphanum = "0123456789abcdefghijklmnopqrstuvwxyz"
	var bytes = make([]byte, n)
	_, _ = rand.Read(bytes)
	for i, b := range bytes {
		bytes[i] = alphanum[b%byte(len(alphanum))]
	}
	return string(bytes)
}

func (s *PublishedStorageSuite) SetUpSuite(c *C) {
	s.accountName = os.Getenv("AZURE_STORAGE_ACCOUNT")
	if s.accountName == "" {
		println("Please set the following two environment variables to run the Azure storage tests.")
		println("  1. AZURE_STORAGE_ACCOUNT")
		println("  2. AZURE_STORAGE_ACCESS_KEY")
		c.Skip("AZURE_STORAGE_ACCOUNT not set.")
	}
	s.accountKey = os.Getenv("AZURE_STORAGE_ACCESS_KEY")
	if s.accountKey == "" {
		println("Please set the following two environment variables to run the Azure storage tests.")
		println("  1. AZURE_STORAGE_ACCOUNT")
		println("  2. AZURE_STORAGE_ACCESS_KEY")
		c.Skip("AZURE_STORAGE_ACCESS_KEY not set.")
	}
	s.endpoint = os.Getenv("AZURE_STORAGE_ENDPOINT")
}

func (s *PublishedStorageSuite) SetUpTest(c *C) {
	container := randContainer()
	prefix := "lala"

	var err error

	s.storage, err = NewPublishedStorage(s.accountName, s.accountKey, container, "", s.endpoint)
	c.Assert(err, IsNil)
        publicAccessType := azblob.PublicAccessTypeContainer
        _, err = s.storage.az.client.CreateContainer(context.Background(), s.storage.az.container, &azblob.CreateContainerOptions{
            Access: &publicAccessType,
        })
	c.Assert(err, IsNil)

	s.prefixedStorage, err = NewPublishedStorage(s.accountName, s.accountKey, container, prefix, s.endpoint)
	c.Assert(err, IsNil)
}

func (s *PublishedStorageSuite) TearDownTest(c *C) {
        _, err := s.storage.az.client.DeleteContainer(context.Background(), s.storage.az.container, nil)
	c.Assert(err, IsNil)
}

func (s *PublishedStorageSuite) GetFile(c *C, path string) []byte {
        resp, err := s.storage.az.client.DownloadStream(context.Background(), s.storage.az.container, path, nil)
	c.Assert(err, IsNil)
	data, err := io.ReadAll(resp.Body)
	c.Assert(err, IsNil)
	return data
}

func (s *PublishedStorageSuite) AssertNoFile(c *C, path string) {
        serviceClient := s.storage.az.client.ServiceClient()
        containerClient := serviceClient.NewContainerClient(s.storage.az.container)
        blobClient := containerClient.NewBlobClient(path)
        _, err := blobClient.GetProperties(context.Background(), nil)
	c.Assert(err, NotNil)

        storageError, ok := err.(*azcore.ResponseError)
	c.Assert(ok, Equals, true)
	c.Assert(storageError.StatusCode, Equals, 404)
}

func (s *PublishedStorageSuite) PutFile(c *C, path string, data []byte) {
	hash := md5.Sum(data)
        uploadOptions := &azblob.UploadStreamOptions{
            HTTPHeaders: &blob.HTTPHeaders{
            	BlobContentMD5: hash[:],
            },
        }
        reader := bytes.NewReader(data)
        _, err := s.storage.az.client.UploadStream(context.Background(), s.storage.az.container, path, reader, uploadOptions)
	c.Assert(err, IsNil)
}

func (s *PublishedStorageSuite) TestPutFile(c *C) {
	content := []byte("Welcome to Azure!")
	filename := "a/b.txt"

	dir := c.MkDir()
	err := os.WriteFile(filepath.Join(dir, "a"), content, 0644)
	c.Assert(err, IsNil)

	err = s.storage.PutFile(filename, filepath.Join(dir, "a"))
	c.Check(err, IsNil)

	c.Check(s.GetFile(c, filename), DeepEquals, content)

	err = s.prefixedStorage.PutFile(filename, filepath.Join(dir, "a"))
	c.Check(err, IsNil)

	c.Check(s.GetFile(c, filepath.Join(s.prefixedStorage.az.prefix, filename)), DeepEquals, content)
}

func (s *PublishedStorageSuite) TestPutFilePlus(c *C) {
	content := []byte("Welcome to Azure!")
	filename := "a/b+c.txt"

	dir := c.MkDir()
	err := os.WriteFile(filepath.Join(dir, "a"), content, 0644)
	c.Assert(err, IsNil)

	err = s.storage.PutFile(filename, filepath.Join(dir, "a"))
	c.Check(err, IsNil)

	c.Check(s.GetFile(c, filename), DeepEquals, content)
	s.AssertNoFile(c, "a/b c.txt")
}

func (s *PublishedStorageSuite) TestFilelist(c *C) {
	paths := []string{"a", "b", "c", "testa", "test/a", "test/b", "lala/a", "lala/b", "lala/c"}
	for _, path := range paths {
		s.PutFile(c, path, []byte("test"))
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

func (s *PublishedStorageSuite) TestFilelistPlus(c *C) {
	paths := []string{"a", "b", "c", "testa", "test/a+1", "test/a 1", "lala/a+b", "lala/a b", "lala/c"}
	for _, path := range paths {
		s.PutFile(c, path, []byte("test"))
	}

	list, err := s.storage.Filelist("")
	c.Check(err, IsNil)
	c.Check(list, DeepEquals, []string{"a", "b", "c", "lala/a b", "lala/a+b", "lala/c", "test/a 1", "test/a+1", "testa"})

	list, err = s.storage.Filelist("test")
	c.Check(err, IsNil)
	c.Check(list, DeepEquals, []string{"a 1", "a+1"})

	list, err = s.storage.Filelist("test2")
	c.Check(err, IsNil)
	c.Check(list, DeepEquals, []string{})

	list, err = s.prefixedStorage.Filelist("")
	c.Check(err, IsNil)
	c.Check(list, DeepEquals, []string{"a b", "a+b", "c"})
}

func (s *PublishedStorageSuite) TestRemove(c *C) {
	s.PutFile(c, "a/b", []byte("test"))

	err := s.storage.Remove("a/b")
	c.Check(err, IsNil)

	s.AssertNoFile(c, "a/b")

	s.PutFile(c, "lala/xyz", []byte("test"))

	err = s.prefixedStorage.Remove("xyz")
	c.Check(err, IsNil)

	s.AssertNoFile(c, "lala/xyz")
}

func (s *PublishedStorageSuite) TestRemovePlus(c *C) {
	s.PutFile(c, "a/b+c", []byte("test"))
	s.PutFile(c, "a/b", []byte("test"))

	err := s.storage.Remove("a/b+c")
	c.Check(err, IsNil)

	s.AssertNoFile(c, "a/b+c")
	s.AssertNoFile(c, "a/b c")

	err = s.storage.Remove("a/b")
	c.Check(err, IsNil)

	s.AssertNoFile(c, "a/b")
}

func (s *PublishedStorageSuite) TestRemoveDirs(c *C) {
	paths := []string{"a", "b", "c", "testa", "test/a", "test/b", "lala/a", "lala/b", "lala/c"}
	for _, path := range paths {
		s.PutFile(c, path, []byte("test"))
	}

	err := s.storage.RemoveDirs("test", nil)
	c.Check(err, IsNil)

	list, err := s.storage.Filelist("")
	c.Check(err, IsNil)
	c.Check(list, DeepEquals, []string{"a", "b", "c", "lala/a", "lala/b", "lala/c", "testa"})
}

func (s *PublishedStorageSuite) TestRemoveDirsPlus(c *C) {
	paths := []string{"a", "b", "c", "testa", "test/a+1", "test/a 1", "lala/a+b", "lala/a b", "lala/c"}
	for _, path := range paths {
		s.PutFile(c, path, []byte("test"))
	}

	err := s.storage.RemoveDirs("test", nil)
	c.Check(err, IsNil)

	list, err := s.storage.Filelist("")
	c.Check(err, IsNil)
	c.Check(list, DeepEquals, []string{"a", "b", "c", "lala/a b", "lala/a+b", "lala/c", "testa"})
}

func (s *PublishedStorageSuite) TestRenameFile(c *C) {
	dir := c.MkDir()
	err := os.WriteFile(filepath.Join(dir, "a"), []byte("Welcome to Azure!"), 0644)
	c.Assert(err, IsNil)

	err = s.storage.PutFile("source.txt", filepath.Join(dir, "a"))
	c.Check(err, IsNil)

	err = s.storage.RenameFile("source.txt", "dest.txt")
	c.Check(err, IsNil)

	c.Check(s.GetFile(c, "dest.txt"), DeepEquals, []byte("Welcome to Azure!"))

	exists, err := s.storage.FileExists("source.txt")
	c.Check(err, IsNil)
	c.Check(exists, Equals, false)
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
	err = s.storage.LinkFromPool("", filepath.Join("", "pool", "main", "m/mars-invaders"), "mars-invaders_1.03.deb", pool, src1, cksum1, false)
	c.Check(err, IsNil)

	c.Check(s.GetFile(c, "pool/main/m/mars-invaders/mars-invaders_1.03.deb"), DeepEquals, []byte("Contents"))

	// duplicate link from pool
	err = s.storage.LinkFromPool("", filepath.Join("pool", "main", "m/mars-invaders"), "mars-invaders_1.03.deb", pool, src1, cksum1, false)
	c.Check(err, IsNil)

	c.Check(s.GetFile(c, "pool/main/m/mars-invaders/mars-invaders_1.03.deb"), DeepEquals, []byte("Contents"))

	// link from pool with conflict
	err = s.storage.LinkFromPool("", filepath.Join("pool", "main", "m/mars-invaders"), "mars-invaders_1.03.deb", pool, src2, cksum2, false)
	c.Check(err, ErrorMatches, ".*file already exists and is different.*")

	c.Check(s.GetFile(c, "pool/main/m/mars-invaders/mars-invaders_1.03.deb"), DeepEquals, []byte("Contents"))

	// link from pool with conflict and force
	err = s.storage.LinkFromPool("", filepath.Join("pool", "main", "m/mars-invaders"), "mars-invaders_1.03.deb", pool, src2, cksum2, true)
	c.Check(err, IsNil)

	c.Check(s.GetFile(c, "pool/main/m/mars-invaders/mars-invaders_1.03.deb"), DeepEquals, []byte("Spam"))

	// for prefixed storage:
	// first link from pool
	err = s.prefixedStorage.LinkFromPool("", filepath.Join("pool", "main", "m/mars-invaders"), "mars-invaders_1.03.deb", pool, src1, cksum1, false)
	c.Check(err, IsNil)

	// 2nd link from pool, providing wrong path for source file
	//
	// this test should check that file already exists in Azure and skip upload (which would fail if not skipped)
	s.prefixedStorage.pathCache = nil
	err = s.prefixedStorage.LinkFromPool("", filepath.Join("pool", "main", "m/mars-invaders"), "mars-invaders_1.03.deb", pool, "wrong-looks-like-pathcache-doesnt-work", cksum1, false)
	c.Check(err, IsNil)

	c.Check(s.GetFile(c, "lala/pool/main/m/mars-invaders/mars-invaders_1.03.deb"), DeepEquals, []byte("Contents"))

	// link from pool with nested file name
	err = s.storage.LinkFromPool("", "dists/jessie/non-free/installer-i386/current/images", "netboot/boot.img.gz", pool, src3, cksum3, false)
	c.Check(err, IsNil)

	c.Check(s.GetFile(c, "dists/jessie/non-free/installer-i386/current/images/netboot/boot.img.gz"), DeepEquals, []byte("Contents"))
}

func (s *PublishedStorageSuite) TestSymLink(c *C) {
	s.PutFile(c, "a/b", []byte("test"))

	err := s.storage.SymLink("a/b", "a/b.link")
	c.Check(err, IsNil)

	var link string
	link, err = s.storage.ReadLink("a/b.link")
	c.Check(err, IsNil)
	c.Check(link, Equals, "a/b")

	c.Skip("copy not available in azure test")
}

func (s *PublishedStorageSuite) TestFileExists(c *C) {
	s.PutFile(c, "a/b", []byte("test"))

	exists, err := s.storage.FileExists("a/b")
	c.Check(err, IsNil)
	c.Check(exists, Equals, true)

	exists, _ = s.storage.FileExists("a/b.invalid")
	c.Check(err, IsNil)
	c.Check(exists, Equals, false)
}
