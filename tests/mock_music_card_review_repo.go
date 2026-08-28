package tests

import (
	"errors"

	"github.com/deluan/rest"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/id"
)

type MockMusicCardReviewRepo struct {
	model.MusicCardReviewRepository
	Data map[string]*model.MusicCardReview // keyed by card_id (the natural key)
	Err  bool
}

func CreateMockMusicCardReviewRepo() *MockMusicCardReviewRepo {
	return &MockMusicCardReviewRepo{Data: map[string]*model.MusicCardReview{}}
}

func (m *MockMusicCardReviewRepo) SetError(err bool) {
	m.Err = err
}

func (m *MockMusicCardReviewRepo) CountAll(options ...model.QueryOptions) (int64, error) {
	if m.Err {
		return 0, errors.New("error")
	}
	return int64(len(m.Data)), nil
}

func (m *MockMusicCardReviewRepo) Count(options ...rest.QueryOptions) (int64, error) {
	return m.CountAll()
}

func (m *MockMusicCardReviewRepo) Get(id string) (*model.MusicCardReview, error) {
	if m.Err {
		return nil, errors.New("error")
	}
	for _, d := range m.Data {
		if d.ID == id {
			return d, nil
		}
	}
	return nil, model.ErrNotFound
}

func (m *MockMusicCardReviewRepo) GetAll(options ...model.QueryOptions) (model.MusicCardReviews, error) {
	if m.Err {
		return nil, errors.New("error")
	}
	all := model.MusicCardReviews{}
	for _, d := range m.Data {
		all = append(all, *d)
	}
	return all, nil
}

func (m *MockMusicCardReviewRepo) GetByCardID(cardID string) (*model.MusicCardReview, error) {
	if m.Err {
		return nil, errors.New("error")
	}
	if d, ok := m.Data[cardID]; ok {
		return d, nil
	}
	return nil, model.ErrNotFound
}

func (m *MockMusicCardReviewRepo) Put(rev *model.MusicCardReview) error {
	if m.Err {
		return errors.New("error")
	}
	if existing, ok := m.Data[rev.CardID]; ok {
		rev.ID = existing.ID
	} else if rev.ID == "" {
		rev.ID = id.NewRandom()
	}
	m.Data[rev.CardID] = rev
	return nil
}

func (m *MockMusicCardReviewRepo) EntityName() string {
	return "music_card_review"
}

func (m *MockMusicCardReviewRepo) NewInstance() any {
	return &model.MusicCardReview{}
}

func (m *MockMusicCardReviewRepo) Read(id string) (any, error) {
	return m.Get(id)
}

func (m *MockMusicCardReviewRepo) ReadAll(options ...rest.QueryOptions) (any, error) {
	return m.GetAll()
}

var _ model.MusicCardReviewRepository = (*MockMusicCardReviewRepo)(nil)
