package tests

import (
	"errors"

	"github.com/deluan/rest"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/id"
)

type MockFuriganaBindingRepo struct {
	model.FuriganaBindingRepository
	Data map[string]*model.FuriganaBinding
	Err  bool
}

func CreateMockFuriganaBindingRepo() *MockFuriganaBindingRepo {
	return &MockFuriganaBindingRepo{Data: map[string]*model.FuriganaBinding{}}
}

func (m *MockFuriganaBindingRepo) SetError(err bool) {
	m.Err = err
}

func (m *MockFuriganaBindingRepo) CountAll(options ...model.QueryOptions) (int64, error) {
	if m.Err {
		return 0, errors.New("error")
	}
	return int64(len(m.Data)), nil
}

func (m *MockFuriganaBindingRepo) Count(options ...rest.QueryOptions) (int64, error) {
	return m.CountAll()
}

func (m *MockFuriganaBindingRepo) Get(id string) (*model.FuriganaBinding, error) {
	if m.Err {
		return nil, errors.New("error")
	}
	if d, ok := m.Data[id]; ok {
		return d, nil
	}
	return nil, model.ErrNotFound
}

func (m *MockFuriganaBindingRepo) GetAll(options ...model.QueryOptions) (model.FuriganaBindings, error) {
	if m.Err {
		return nil, errors.New("error")
	}
	all := model.FuriganaBindings{}
	for _, d := range m.Data {
		all = append(all, *d)
	}
	return all, nil
}

func (m *MockFuriganaBindingRepo) Put(fb *model.FuriganaBinding) error {
	if m.Err {
		return errors.New("error")
	}
	if fb.ID == "" {
		fb.ID = id.NewRandom()
	}
	m.Data[fb.ID] = fb
	return nil
}

func (m *MockFuriganaBindingRepo) Delete(id string) error {
	if m.Err {
		return errors.New("error")
	}
	if _, found := m.Data[id]; !found {
		return model.ErrNotFound
	}
	delete(m.Data, id)
	return nil
}

func (m *MockFuriganaBindingRepo) EntityName() string {
	return "furigana_binding"
}

func (m *MockFuriganaBindingRepo) NewInstance() any {
	return &model.FuriganaBinding{}
}

func (m *MockFuriganaBindingRepo) Read(id string) (any, error) {
	return m.Get(id)
}

func (m *MockFuriganaBindingRepo) ReadAll(options ...rest.QueryOptions) (any, error) {
	return m.GetAll()
}

func (m *MockFuriganaBindingRepo) Save(entity any) (string, error) {
	fb := entity.(*model.FuriganaBinding)
	if err := m.Put(fb); err != nil {
		return "", err
	}
	return fb.ID, nil
}

func (m *MockFuriganaBindingRepo) Update(id string, entity any, cols ...string) error {
	if _, found := m.Data[id]; !found {
		return rest.ErrNotFound
	}
	fb := entity.(*model.FuriganaBinding)
	fb.ID = id
	m.Data[id] = fb
	return nil
}

var _ model.FuriganaBindingRepository = (*MockFuriganaBindingRepo)(nil)
