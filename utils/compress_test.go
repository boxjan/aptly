package utils

import (
	"compress/bzip2"
	"compress/gzip"
	"io"
	"os"

	. "gopkg.in/check.v1"
)

type CompressSuite struct {
	tempfile *os.File
}

var _ = Suite(&CompressSuite{})

const testString = "Quick brown fox jumps over black dog and runs away... Really far away... who knows?"

func (s *CompressSuite) SetUpTest(c *C) {
	s.tempfile, _ = os.CreateTemp(c.MkDir(), "aptly-test")
	_, _ = s.tempfile.WriteString(testString)
}

func (s *CompressSuite) TearDownTest(c *C) {
	_ = s.tempfile.Close()
}

func (s *CompressSuite) TestCompress(c *C) {
	err := CompressFile(s.tempfile, false)
	c.Assert(err, IsNil)

	file, err := os.Open(s.tempfile.Name() + ".gz")
	c.Assert(err, IsNil)

	gzReader, err := gzip.NewReader(file)
	c.Assert(err, IsNil)

	buf, err := io.ReadAll(gzReader)
	c.Assert(err, IsNil)

	_ = gzReader.Close()
	_ = file.Close()

	c.Check(string(buf), Equals, testString)

	file, err = os.Open(s.tempfile.Name() + ".bz2")
	c.Assert(err, IsNil)

	bzReader := bzip2.NewReader(file)

	_, err = bzReader.Read(buf)
	c.Assert(err, IsNil)

	_ = file.Close()

	c.Check(string(buf), Equals, testString)
}
