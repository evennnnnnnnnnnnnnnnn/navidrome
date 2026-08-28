package tests

import (
	"errors"

	"github.com/deluan/rest"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/id"
)

type MockMusicCardSnippetRepo struct {
	model.MusicCardSnippetRepository
	Data map[string]*model.MusicCardSnippet
	Err  bool
}

func CreateMockMusicCardSnippetRepo() *MockMusicCardSnippetRepo {
	return &MockMusicCardSnippetRepo{Data: map[string]*model.MusicCardSnippet{}}
}

func (m *MockMusicCardSnippetRepo) SetError(err bool) {
	m.Err = err
}

func (m *MockMusicCardSnippetRepo) CountAll(options ...model.QueryOptions) (int64, error) {
	if m.Err {
		return 0, errors.New("error")
	}
	return int64(len(m.Data)), nil
}

func (m *MockMusicCardSnippetRepo) Count(options ...rest.QueryOptions) (int64, error) {
	return m.CountAll()
}

func (m *MockMusicCardSnippetRepo) Get(id string) (*model.MusicCardSnippet, error) {
	if m.Err {
		return nil, errors.New("error")
	}
	if d, ok := m.Data[id]; ok {
		return d, nil
	}
	return nil, model.ErrNotFound
}

func (m *MockMusicCardSnippetRepo) GetAll(options ...model.QueryOptions) (model.MusicCardSnippets, error) {
	if m.Err {
		return nil, errors.New("error")
	}
	all := model.MusicCardSnippets{}
	for _, d := range m.Data {
		all = append(all, *d)
	}
	return all, nil
}

func (m *MockMusicCardSnippetRepo) Put(s *model.MusicCardSnippet) error {
	if m.Err {
		return errors.New("error")
	}
	if s.ID == "" {
		s.ID = id.NewRandom()
	}
	m.Data[s.ID] = s
	return nil
}

func (m *MockMusicCardSnippetRepo) Delete(id string) error {
	if m.Err {
		return errors.New("error")
	}
	if _, found := m.Data[id]; !found {
		return model.ErrNotFound
	}
	delete(m.Data, id)
	return nil
}

func (m *MockMusicCardSnippetRepo) EntityName() string {
	return "music_card_snippet"
}

func (m *MockMusicCardSnippetRepo) NewInstance() any {
	return &model.MusicCardSnippet{}
}

func (m *MockMusicCardSnippetRepo) Read(id string) (any, error) {
	return m.Get(id)
}

func (m *MockMusicCardSnippetRepo) ReadAll(options ...rest.QueryOptions) (any, error) {
	return m.GetAll()
}

func (m *MockMusicCardSnippetRepo) Save(entity any) (string, error) {
	s := entity.(*model.MusicCardSnippet)
	if err := m.Put(s); err != nil {
		return "", err
	}
	return s.ID, nil
}

func (m *MockMusicCardSnippetRepo) Update(id string, entity any, cols ...string) error {
	if _, found := m.Data[id]; !found {
		return rest.ErrNotFound
	}
	s := entity.(*model.MusicCardSnippet)
	s.ID = id
	m.Data[id] = s
	return nil
}

var _ model.MusicCardSnippetRepository = (*MockMusicCardSnippetRepo)(nil)
