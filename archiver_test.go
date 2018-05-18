package archiver

import (
	"compress/gzip"
	"context"
	"io/ioutil"
	"os"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/nyaruka/ezconf"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func setup(t *testing.T) *sqlx.DB {
	testDB, err := ioutil.ReadFile("testdb.sql")
	assert.NoError(t, err)

	db, err := sqlx.Open("postgres", "postgres://localhost/archiver_test?sslmode=disable")
	assert.NoError(t, err)

	_, err = db.Exec(string(testDB))
	assert.NoError(t, err)
	logrus.SetLevel(logrus.DebugLevel)

	return db
}

func TestGetMissingDayArchives(t *testing.T) {
	db := setup(t)

	// get the tasks for our org
	ctx := context.Background()
	orgs, err := GetActiveOrgs(ctx, db)
	assert.NoError(t, err)

	existing, err := GetCurrentArchives(ctx, db, orgs[0], MessageType)
	assert.NoError(t, err)
	now := time.Date(2018, 1, 8, 12, 30, 0, 0, time.UTC)

	// org 1 is too new, no tasks
	tasks, err := GetMissingDayArchives(existing, now, orgs[0], MessageType)
	assert.NoError(t, err)
	assert.Equal(t, 0, len(tasks))

	// org 2 should have some
	existing, err = GetCurrentArchives(ctx, db, orgs[1], MessageType)
	assert.NoError(t, err)
	tasks, err = GetMissingDayArchives(existing, now, orgs[1], MessageType)
	assert.NoError(t, err)
	assert.Equal(t, 62, len(tasks))
	assert.Equal(t, time.Date(2017, 8, 10, 0, 0, 0, 0, time.UTC), tasks[0].StartDate)
	assert.Equal(t, time.Date(2017, 10, 10, 0, 0, 0, 0, time.UTC), tasks[61].StartDate)

	// org 3 is the same as 2, but two of the tasks have already been built
	existing, err = GetCurrentArchives(ctx, db, orgs[2], MessageType)
	assert.NoError(t, err)
	tasks, err = GetMissingDayArchives(existing, now, orgs[2], MessageType)
	assert.NoError(t, err)
	assert.Equal(t, 60, len(tasks))
	assert.Equal(t, time.Date(2017, 8, 11, 0, 0, 0, 0, time.UTC), tasks[0].StartDate)
	assert.Equal(t, time.Date(2017, 10, 10, 0, 0, 0, 0, time.UTC), tasks[59].StartDate)
}

func TestGetMissingMonthArchives(t *testing.T) {
	db := setup(t)

	// get the tasks for our org
	ctx := context.Background()
	orgs, err := GetActiveOrgs(ctx, db)
	assert.NoError(t, err)

	existing, err := GetCurrentArchives(ctx, db, orgs[0], MessageType)
	assert.NoError(t, err)
	now := time.Date(2018, 1, 8, 12, 30, 0, 0, time.UTC)

	// org 1 is too new, no tasks
	tasks, err := GetMissingMonthArchives(existing, now, orgs[0], MessageType)
	assert.NoError(t, err)
	assert.Equal(t, 0, len(tasks))

	// org 2 should have some
	existing, err = GetCurrentArchives(ctx, db, orgs[1], MessageType)
	assert.NoError(t, err)
	tasks, err = GetMissingMonthArchives(existing, now, orgs[1], MessageType)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(tasks))
	assert.Equal(t, time.Date(2017, 8, 1, 0, 0, 0, 0, time.UTC), tasks[0].StartDate)
	assert.Equal(t, time.Date(2017, 9, 1, 0, 0, 0, 0, time.UTC), tasks[1].StartDate)

	// org 3 is the same as 2, but two of the tasks have already been built
	existing, err = GetCurrentArchives(ctx, db, orgs[2], MessageType)
	assert.NoError(t, err)
	tasks, err = GetMissingMonthArchives(existing, now, orgs[2], MessageType)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(tasks))
	assert.Equal(t, time.Date(2017, 8, 1, 0, 0, 0, 0, time.UTC), tasks[0].StartDate)
}

func TestCreateMsgArchive(t *testing.T) {
	db := setup(t)
	ctx := context.Background()

	err := EnsureTempArchiveDirectory("/tmp")
	assert.NoError(t, err)

	orgs, err := GetActiveOrgs(ctx, db)
	assert.NoError(t, err)
	now := time.Date(2018, 1, 8, 12, 30, 0, 0, time.UTC)

	existing, err := GetCurrentArchives(ctx, db, orgs[1], MessageType)
	assert.NoError(t, err)
	tasks, err := GetMissingDayArchives(existing, now, orgs[1], MessageType)
	assert.NoError(t, err)
	assert.Equal(t, 62, len(tasks))
	task := tasks[0]

	// build our first task, should have no messages
	err = CreateArchiveFile(ctx, db, task, "/tmp")
	assert.NoError(t, err)

	// should have no records and be an empty gzip file
	assert.Equal(t, 0, task.RecordCount)
	assert.Equal(t, int64(23), task.Size)
	assert.Equal(t, "f0d79988b7772c003d04a28bd7417a62", task.Hash)

	DeleteArchiveFile(task)

	// build our third task, should have a single message
	task = tasks[2]
	err = CreateArchiveFile(ctx, db, task, "/tmp")
	assert.NoError(t, err)

	// should have two records, second will have attachments
	assert.Equal(t, 2, task.RecordCount)
	assert.Equal(t, int64(448), task.Size)
	assert.Equal(t, "74ab5f70262ccd7b10ef0ae7274c806d", task.Hash)
	assertArchiveFile(t, task, "messages1.jsonl")

	DeleteArchiveFile(task)
	_, err = os.Stat(task.ArchiveFile)
	assert.True(t, os.IsNotExist(err))

	// test the anonymous case
	existing, err = GetCurrentArchives(ctx, db, orgs[2], MessageType)
	assert.NoError(t, err)
	tasks, err = GetMissingDayArchives(existing, now, orgs[2], MessageType)
	assert.NoError(t, err)
	assert.Equal(t, 60, len(tasks))
	task = tasks[0]

	// build our first task, should have no messages
	err = CreateArchiveFile(ctx, db, task, "/tmp")
	assert.NoError(t, err)

	// should have no records and be an empty gzip file
	assert.Equal(t, 1, task.RecordCount)
	assert.Equal(t, int64(283), task.Size)
	assert.Equal(t, "d03b1ab8d3312b37d5e0ae38b88e1ea7", task.Hash)
	assertArchiveFile(t, task, "messages2.jsonl")

	DeleteArchiveFile(task)
}

func assertArchiveFile(t *testing.T, archive *Archive, truthName string) {
	testFile, err := os.Open(archive.ArchiveFile)
	assert.NoError(t, err)

	zTestReader, err := gzip.NewReader(testFile)
	assert.NoError(t, err)
	test, err := ioutil.ReadAll(zTestReader)
	assert.NoError(t, err)

	truth, err := ioutil.ReadFile("./testdata/" + truthName)
	assert.NoError(t, err)

	assert.Equal(t, truth, test)
}

func TestCreateRunArchive(t *testing.T) {
	db := setup(t)
	ctx := context.Background()

	err := EnsureTempArchiveDirectory("/tmp")
	assert.NoError(t, err)

	orgs, err := GetActiveOrgs(ctx, db)
	assert.NoError(t, err)
	now := time.Date(2018, 1, 8, 12, 30, 0, 0, time.UTC)

	existing, err := GetCurrentArchives(ctx, db, orgs[1], RunType)
	assert.NoError(t, err)
	tasks, err := GetMissingDayArchives(existing, now, orgs[1], RunType)
	assert.NoError(t, err)
	assert.Equal(t, 62, len(tasks))
	task := tasks[0]

	// build our first task, should have no messages
	err = CreateArchiveFile(ctx, db, task, "/tmp")
	assert.NoError(t, err)

	// should have no records and be an empty gzip file
	assert.Equal(t, 0, task.RecordCount)
	assert.Equal(t, int64(23), task.Size)
	assert.Equal(t, "f0d79988b7772c003d04a28bd7417a62", task.Hash)

	DeleteArchiveFile(task)

	// build our third task, should have a single message
	task = tasks[2]
	err = CreateArchiveFile(ctx, db, task, "/tmp")
	assert.NoError(t, err)

	// should have two record
	assert.Equal(t, 2, task.RecordCount)
	assert.Equal(t, int64(568), task.Size)
	assert.Equal(t, "830b11f3653e4c961fe714fb425d4cec", task.Hash)
	assertArchiveFile(t, task, "runs1.jsonl")

	DeleteArchiveFile(task)
	_, err = os.Stat(task.ArchiveFile)
	assert.True(t, os.IsNotExist(err))

	// ok, let's do an anon org
	existing, err = GetCurrentArchives(ctx, db, orgs[2], RunType)
	assert.NoError(t, err)
	tasks, err = GetMissingDayArchives(existing, now, orgs[2], RunType)
	assert.NoError(t, err)
	assert.Equal(t, 62, len(tasks))
	task = tasks[0]

	// build our first task, should have no messages
	err = CreateArchiveFile(ctx, db, task, "/tmp")
	assert.NoError(t, err)

	// should have one record
	assert.Equal(t, 1, task.RecordCount)
	assert.Equal(t, int64(389), task.Size)
	assert.Equal(t, "d356e67393a5ae9c0fc07f81739c9d03", task.Hash)
	assertArchiveFile(t, task, "runs2.jsonl")

	DeleteArchiveFile(task)
}

func TestWriteArchiveToDB(t *testing.T) {
	db := setup(t)
	ctx := context.Background()

	orgs, err := GetActiveOrgs(ctx, db)
	assert.NoError(t, err)
	now := time.Date(2018, 1, 8, 12, 30, 0, 0, time.UTC)

	existing, err := GetCurrentArchives(ctx, db, orgs[2], MessageType)
	assert.NoError(t, err)

	tasks, err := GetMissingDayArchives(existing, now, orgs[2], MessageType)
	assert.NoError(t, err)
	assert.Equal(t, 60, len(tasks))
	assert.Equal(t, time.Date(2017, 8, 11, 0, 0, 0, 0, time.UTC), tasks[0].StartDate)

	task := tasks[0]
	task.Dailies = []*Archive{existing[0], existing[1]}

	err = WriteArchiveToDB(ctx, db, task)

	assert.NoError(t, err)
	assert.Equal(t, 4, task.ID)
	assert.Equal(t, false, task.IsPurged)

	// if we recalculate our tasks, we should have one less now
	existing, err = GetCurrentArchives(ctx, db, orgs[2], MessageType)
	assert.Equal(t, task.ID, *existing[0].Rollup)
	assert.Equal(t, task.ID, *existing[2].Rollup)

	assert.NoError(t, err)
	tasks, err = GetMissingDayArchives(existing, now, orgs[2], MessageType)
	assert.NoError(t, err)
	assert.Equal(t, 59, len(tasks))
	assert.Equal(t, time.Date(2017, 8, 12, 0, 0, 0, 0, time.UTC), tasks[0].StartDate)
}

func TestArchiveOrgMessages(t *testing.T) {
	db := setup(t)
	ctx := context.Background()

	orgs, err := GetActiveOrgs(ctx, db)
	assert.NoError(t, err)
	now := time.Date(2018, 1, 8, 12, 30, 0, 0, time.UTC)

	config := NewConfig()
	os.Args = []string{"rp-archiver"}

	loader := ezconf.NewLoader(&config, "archiver", "Archives RapidPro runs and msgs to S3", nil)
	loader.MustLoad()

	// AWS S3 config in the environment needed to download from S3
	if config.AWSAccessKeyID != "missing_aws_access_key_id" && config.AWSSecretAccessKey != "missing_aws_secret_access_key" {

		s3Client, err := NewS3Client(config)
		assert.NoError(t, err)

		archives, err := ArchiveOrg(ctx, now, config, db, s3Client, orgs[1], MessageType)
		assert.NoError(t, err)

		assert.Equal(t, 64, len(archives))
		assert.Equal(t, time.Date(2017, 8, 10, 0, 0, 0, 0, time.UTC), archives[0].StartDate)
		assert.Equal(t, time.Date(2017, 10, 10, 0, 0, 0, 0, time.UTC), archives[61].StartDate)
		assert.Equal(t, time.Date(2017, 8, 1, 0, 0, 0, 0, time.UTC), archives[62].StartDate)
		assert.Equal(t, time.Date(2017, 9, 1, 0, 0, 0, 0, time.UTC), archives[63].StartDate)

		assert.Equal(t, 0, archives[0].RecordCount)
		assert.Equal(t, int64(23), archives[0].Size)
		assert.Equal(t, "f0d79988b7772c003d04a28bd7417a62", archives[0].Hash)

		assert.Equal(t, 2, archives[2].RecordCount)
		assert.Equal(t, int64(448), archives[2].Size)
		assert.Equal(t, "74ab5f70262ccd7b10ef0ae7274c806d", archives[2].Hash)

		assert.Equal(t, 1, archives[3].RecordCount)
		assert.Equal(t, int64(299), archives[3].Size)
		assert.Equal(t, "3683faa7b3a546b47b0bac1ec150f8af", archives[3].Hash)

		assert.Equal(t, 3, archives[62].RecordCount)
		assert.Equal(t, int64(470), archives[62].Size)
		assert.Equal(t, "7033bb24efca482d121b8e0cdc6b1430", archives[62].Hash)

		assert.Equal(t, 0, archives[63].RecordCount)
		assert.Equal(t, int64(23), archives[63].Size)
		assert.Equal(t, "f0d79988b7772c003d04a28bd7417a62", archives[63].Hash)
	}
}

func TestArchiveOrgRuns(t *testing.T) {
	db := setup(t)
	ctx := context.Background()

	orgs, err := GetActiveOrgs(ctx, db)
	assert.NoError(t, err)
	now := time.Date(2018, 1, 8, 12, 30, 0, 0, time.UTC)

	config := NewConfig()
	os.Args = []string{"rp-archiver"}

	loader := ezconf.NewLoader(&config, "archiver", "Archives RapidPro runs and msgs to S3", nil)
	loader.MustLoad()

	// AWS S3 config in the environment needed to download from S3
	if config.AWSAccessKeyID != "missing_aws_access_key_id" && config.AWSSecretAccessKey != "missing_aws_secret_access_key" {

		s3Client, err := NewS3Client(config)
		assert.NoError(t, err)

		archives, err := ArchiveOrg(ctx, now, config, db, s3Client, orgs[2], RunType)
		assert.NoError(t, err)

		assert.Equal(t, 64, len(archives))
		assert.Equal(t, time.Date(2017, 8, 10, 0, 0, 0, 0, time.UTC), archives[0].StartDate)
		assert.Equal(t, time.Date(2017, 10, 10, 0, 0, 0, 0, time.UTC), archives[61].StartDate)
		assert.Equal(t, time.Date(2017, 8, 1, 0, 0, 0, 0, time.UTC), archives[62].StartDate)
		assert.Equal(t, time.Date(2017, 9, 1, 0, 0, 0, 0, time.UTC), archives[63].StartDate)

		assert.Equal(t, 1, archives[0].RecordCount)
		assert.Equal(t, int64(389), archives[0].Size)
		assert.Equal(t, "d356e67393a5ae9c0fc07f81739c9d03", archives[0].Hash)

		assert.Equal(t, 0, archives[2].RecordCount)
		assert.Equal(t, int64(23), archives[2].Size)
		assert.Equal(t, "f0d79988b7772c003d04a28bd7417a62", archives[2].Hash)

		assert.Equal(t, 1, archives[62].RecordCount)
		assert.Equal(t, int64(389), archives[62].Size)
		assert.Equal(t, "d356e67393a5ae9c0fc07f81739c9d03", archives[62].Hash)

		assert.Equal(t, 0, archives[63].RecordCount)
		assert.Equal(t, int64(23), archives[63].Size)
		assert.Equal(t, "f0d79988b7772c003d04a28bd7417a62", archives[63].Hash)
	}
}
