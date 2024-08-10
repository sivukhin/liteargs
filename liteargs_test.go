package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLiteArgs(t *testing.T) {
	db, err := NewLiteArgsDb(":memory:")
	require.Nil(t, err)
	require.Nil(t, db.Init([]string{"name", "url"}))
	require.Nil(t, db.Insert([]string{"n-1", "https://google.com"}))
	require.Nil(t, db.Insert([]string{"n-2", "https://example.com"}))
	require.Nil(t, db.Insert([]string{"n-3", "https://turso.tech"}))
	{
		result, _, err := db.Filter(LiteArgsDbFilter{})
		require.Nil(t, err)
		require.Equal(t, result, []map[string]any{
			{"rowid": int64(1), "name": "n-1", "url": "https://google.com"},
			{"rowid": int64(2), "name": "n-2", "url": "https://example.com"},
			{"rowid": int64(3), "name": "n-3", "url": "https://turso.tech"},
		})
	}
	{
		result, _, err := db.Filter(LiteArgsDbFilter{Take: 1})
		require.Nil(t, err)
		require.Equal(t, result, []map[string]any{
			{"rowid": int64(1), "name": "n-1", "url": "https://google.com"},
		})
	}
	{
		result, _, err := db.Filter(LiteArgsDbFilter{Order: "name DESC"})
		require.Nil(t, err)
		require.Equal(t, result, []map[string]any{
			{"rowid": int64(3), "name": "n-3", "url": "https://turso.tech"},
			{"rowid": int64(2), "name": "n-2", "url": "https://example.com"},
			{"rowid": int64(1), "name": "n-1", "url": "https://google.com"},
		})
	}
	{
		result, _, err := db.Filter(LiteArgsDbFilter{Filter: "url LIKE '%.com'"})
		require.Nil(t, err)
		require.Equal(t, result, []map[string]any{
			{"rowid": int64(1), "name": "n-1", "url": "https://google.com"},
			{"rowid": int64(2), "name": "n-2", "url": "https://example.com"},
		})
	}
}
