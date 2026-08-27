package tests

import (
	"errors"

	"github.com/deluan/rest"
	"github.com/navidrome/navidrome/model"
)

type MockLyricsOverrideRepo struct {
	model.LyricsOverrideRepository
	Data             map[string]model.LyricsOverride
	Err              bool
	PermissionDenied bool
}

func CreateMockLyricsOverrideRepo() *MockLyricsOverrideRepo {
	return &MockLyricsOverrideRepo{Data: map[string]model.LyricsOverride{}}
}

func (m *MockLyricsOverrideRepo) Get(mediaFileID string) (*model.LyricsOverride, error) {
	if m.Err {
		return nil, errMockLyricsOverride
	}
	if o, ok := m.Data[mediaFileID]; ok {
		return &o, nil
	}
	return nil, model.ErrNotFound
}

func (m *MockLyricsOverrideRepo) Put(mediaFileID string, lyrics model.LyricList) error {
	if m.PermissionDenied {
		return rest.ErrPermissionDenied
	}
	if m.Err {
		return errMockLyricsOverride
	}
	data, err := lyrics.MarshalJSON()
	if err != nil {
		return err
	}
	m.Data[mediaFileID] = model.LyricsOverride{MediaFileID: mediaFileID, Lyrics: string(data)}
	return nil
}

func (m *MockLyricsOverrideRepo) Delete(mediaFileID string) error {
	if m.PermissionDenied {
		return rest.ErrPermissionDenied
	}
	if m.Err {
		return errMockLyricsOverride
	}
	delete(m.Data, mediaFileID)
	return nil
}

var errMockLyricsOverride = errors.New("mock error")
