package syncer

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/PowerDNS/lmdb-go/lmdb"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
	"powerdns.com/platform/lightningstream/config"
	"powerdns.com/platform/lightningstream/lmdbenv"
	"powerdns.com/platform/lightningstream/snapshot"
)

func b(s string) []byte {
	return []byte(s)
}

func testTS(i int) (uint64, string) {
	tsNano := uint64(time.Date(2022, 2, i, 3, 4, 5, 123456789, time.UTC).UnixNano())
	tsNanoBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(tsNanoBytes, tsNano)
	tsNanoString := string(tsNanoBytes)
	return tsNano, tsNanoString
}

func TestSyncer_mainToShadow(t *testing.T) {
	v1 := snapshot.Snapshot{
		Databases: []*snapshot.DBI{
			{
				Name: "foo",
				Entries: []snapshot.KV{
					{Key: b("a"), Value: b("abc")},
					{Key: b("b"), Value: b("xyz")},
					{Key: b("c"), Value: b("cccccc")},
				},
			},
		},
	}

	ts1, ts1s := testTS(1)
	ts2, ts2s := testTS(2)
	ts3, _ := testTS(3)

	err := lmdbenv.TestEnv(func(env *lmdb.Env) error {
		// Initial data
		err := env.Update(func(txn *lmdb.Txn) error {
			// First insert the initial data into the main database
			dbi, err := txn.OpenDBI("foo", lmdb.Create)
			assert.NoError(t, err)
			for _, e := range v1.Databases[0].Entries {
				err := txn.Put(dbi, e.Key, e.Value, 0)
				assert.NoError(t, err)
			}

			// Copy to shadow
			s := New("test", nil, config.Config{}, config.LMDB{})
			err = s.mainToShadow(context.Background(), env, txn, ts1)
			assert.NoError(t, err)

			// Read shadow DBI
			shadowDBI, err := txn.OpenDBI("_sync_foo", 0)
			assert.NoError(t, err)
			vals, err := lmdbenv.ReadDBIString(txn, shadowDBI)
			assert.NoError(t, err)

			// Verify contents
			assert.Equal(t, []lmdbenv.KVString{
				{Key: "a", Val: ts1s + "abc"},
				{Key: "b", Val: ts1s + "xyz"},
				{Key: "c", Val: ts1s + "cccccc"},
			}, vals)
			return nil
		})
		assert.NoError(t, err)

		// Add and delete something in data and sync again
		err = env.Update(func(txn *lmdb.Txn) error {
			dbi, err := txn.OpenDBI("foo", 0)
			assert.NoError(t, err)
			// Add new 'd'
			err = txn.Put(dbi, b("d"), b("ddd"), 0)
			assert.NoError(t, err)
			// Remove 'b'
			err = txn.Del(dbi, b("b"), nil)
			assert.NoError(t, err)
			// Change 'c'
			err = txn.Put(dbi, b("c"), b("CCC"), 0)
			assert.NoError(t, err)

			// Copy to shadow
			s := New("test", nil, config.Config{}, config.LMDB{})
			err = s.mainToShadow(context.Background(), env, txn, ts2)
			assert.NoError(t, err)

			// Read shadow DBI
			shadowDBI, err := txn.OpenDBI("_sync_foo", 0)
			assert.NoError(t, err)
			vals, err := lmdbenv.ReadDBIString(txn, shadowDBI)
			assert.NoError(t, err)

			// Verify contents
			assert.Equal(t, []lmdbenv.KVString{
				{Key: "a", Val: ts1s + "abc"}, // timestamp unchanged
				{Key: "b", Val: ts2s},         // deleted, empty value
				{Key: "c", Val: ts2s + "CCC"}, // changed
				{Key: "d", Val: ts2s + "ddd"}, // new
			}, vals)
			return nil
		})
		assert.NoError(t, err)

		// No changes in db, so no timestamp changes
		err = env.Update(func(txn *lmdb.Txn) error {
			// Copy to shadow
			s := New("test", nil, config.Config{}, config.LMDB{})
			err = s.mainToShadow(context.Background(), env, txn, ts3)
			assert.NoError(t, err)

			// Read shadow DBI
			shadowDBI, err := txn.OpenDBI("_sync_foo", 0)
			assert.NoError(t, err)
			vals, err := lmdbenv.ReadDBIString(txn, shadowDBI)
			assert.NoError(t, err)

			// Verify contents
			assert.Equal(t, []lmdbenv.KVString{
				{Key: "a", Val: ts1s + "abc"}, // timestamp unchanged
				{Key: "b", Val: ts2s},         // deleted, empty value
				{Key: "c", Val: ts2s + "CCC"}, // changed
				{Key: "d", Val: ts2s + "ddd"}, // new
			}, vals)
			return nil
		})
		assert.NoError(t, err)

		return nil
	})
	assert.NoError(t, err)

}