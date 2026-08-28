package tests

import (
	"errors"

	"github.com/deluan/rest"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/id"
)

type MockMusicCardRepo struct {
	model.MusicCardRepository
	Data map[string]*model.MusicCard
	Err  bool
}

func CreateMockMusicCardRepo() *MockMusicCardRepo {
	return &MockMusicCardRepo{Data: map[string]*model.MusicCard{}}
}

func (m *MockMusicCardRepo) SetError(err bool) {
	m.Err = err
}

func (m *MockMusicCardRepo) CountAll(options ...model.QueryOptions) (int64, error) {
	if m.Err {
		return 0, errors.New("error")
	}
	return int64(len(m.Data)), nil
}

func (m *MockMusicCardRepo) Count(options ...rest.QueryOptions) (int64, error) {
	return m.CountAll()
}

func (m *MockMusicCardRepo) Get(id string) (*model.MusicCard, error) {
	if m.Err {
		return nil, errors.New("error")
	}
	if d, ok := m.Data[id]; ok {
		return d, nil
	}
	return nil, model.ErrNotFound
}

func (m *MockMusicCardRepo) GetAll(options ...model.QueryOptions) (model.MusicCards, error) {
	if m.Err {
		return nil, errors.New("error")
	}
	all := model.MusicCards{}
	for _, d := range m.Data {
		all = append(all, *d)
	}
	return all, nil
}

func (m *MockMusicCardRepo) Put(mc *model.MusicCard) error {
	if m.Err {
		return errors.New("error")
	}
	if mc.ID == "" {
		mc.ID = id.NewRandom()
	}
	m.Data[mc.ID] = mc
	return nil
}

func (m *MockMusicCardRepo) Delete(id string) error {
	if m.Err {
		return errors.New("error")
	}
	if _, found := m.Data[id]; !found {
		return model.ErrNotFound
	}
	delete(m.Data, id)
	return nil
}

func (m *MockMusicCardRepo) EntityName() string {
	return "music_card"
}

func (m *MockMusicCardRepo) NewInstance() any {
	return &model.MusicCard{}
}

func (m *MockMusicCardRepo) Read(id string) (any, error) {
	return m.Get(id)
}

func (m *MockMusicCardRepo) ReadAll(options ...rest.QueryOptions) (any, error) {
	return m.GetAll()
}

func (m *MockMusicCardRepo) Save(entity any) (string, error) {
	mc := entity.(*model.MusicCard)
	if err := m.Put(mc); err != nil {
		return "", err
	}
	return mc.ID, nil
}

func (m *MockMusicCardRepo) Update(id string, entity any, cols ...string) error {
	if _, found := m.Data[id]; !found {
		return rest.ErrNotFound
	}
	mc := entity.(*model.MusicCard)
	mc.ID = id
	m.Data[id] = mc
	return nil
}

var _ model.MusicCardRepository = (*MockMusicCardRepo)(nil)
