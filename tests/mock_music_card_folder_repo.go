package tests

import (
	"errors"
	"slices"

	"github.com/deluan/rest"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/id"
)

type MockMusicCardFolderRepo struct {
	model.MusicCardFolderRepository
	Data map[string]*model.MusicCardFolder
	// CardsData is what Cards returns per folder id; a folder missing from it yields ErrNotFound.
	CardsData map[string]model.MusicCardsWithSnippets
	// Denied folder ids are refused with rest.ErrPermissionDenied on writes, as the real repository
	// does for a folder the caller does not own.
	Denied map[string]bool
	// Hidden folder ids are reported as missing on reads, as the visibility filter does.
	Hidden map[string]bool
	Added  map[string][]string
	Err    bool
}

func CreateMockMusicCardFolderRepo() *MockMusicCardFolderRepo {
	return &MockMusicCardFolderRepo{
		Data:      map[string]*model.MusicCardFolder{},
		CardsData: map[string]model.MusicCardsWithSnippets{},
		Denied:    map[string]bool{},
		Hidden:    map[string]bool{},
		Added:     map[string][]string{},
	}
}

func (m *MockMusicCardFolderRepo) SetError(err bool) {
	m.Err = err
}

func (m *MockMusicCardFolderRepo) CountAll(options ...model.QueryOptions) (int64, error) {
	if m.Err {
		return 0, errors.New("error")
	}
	return int64(len(m.Data)), nil
}

func (m *MockMusicCardFolderRepo) Count(options ...rest.QueryOptions) (int64, error) {
	return m.CountAll()
}

func (m *MockMusicCardFolderRepo) Get(id string) (*model.MusicCardFolder, error) {
	if m.Err {
		return nil, errors.New("error")
	}
	if d, ok := m.Data[id]; ok && !m.Hidden[id] {
		return d, nil
	}
	return nil, model.ErrNotFound
}

func (m *MockMusicCardFolderRepo) GetAll(options ...model.QueryOptions) (model.MusicCardFolders, error) {
	if m.Err {
		return nil, errors.New("error")
	}
	all := model.MusicCardFolders{}
	for _, d := range m.Data {
		all = append(all, *d)
	}
	return all, nil
}

func (m *MockMusicCardFolderRepo) Put(f *model.MusicCardFolder) error {
	if m.Err {
		return errors.New("error")
	}
	if f.ID == "" {
		f.ID = id.NewRandom()
	}
	f.IsDefault = false // is_default is server-owned, never taken from a payload
	if err := m.checkNameFree(f.ID, f.Name); err != nil {
		return err
	}
	m.Data[f.ID] = f
	return nil
}

func (m *MockMusicCardFolderRepo) Delete(id string) error {
	if m.Err {
		return errors.New("error")
	}
	f, found := m.Data[id]
	if !found {
		return rest.ErrNotFound
	}
	if f.IsDefault {
		return &rest.ValidationError{Errors: map[string]string{"is_default": "the default folder cannot be deleted"}}
	}
	delete(m.Data, id)
	return nil
}

func (m *MockMusicCardFolderRepo) EnsureDefault() (*model.MusicCardFolder, error) {
	if m.Err {
		return nil, errors.New("error")
	}
	for _, f := range m.Data {
		if f.IsDefault {
			return f, nil
		}
	}
	f := &model.MusicCardFolder{ID: id.NewRandom(), Name: "Default", IsDefault: true, ReviewEnabled: true}
	m.Data[f.ID] = f
	return f, nil
}

func (m *MockMusicCardFolderRepo) AddCardToDefault(cardID string) error {
	f, err := m.EnsureDefault()
	if err != nil {
		return err
	}
	return m.AddCards(f.ID, []string{cardID})
}

func (m *MockMusicCardFolderRepo) AddCards(folderID string, cardIDs []string) error {
	if m.Err {
		return errors.New("error")
	}
	if m.Denied[folderID] {
		return rest.ErrPermissionDenied
	}
	if _, found := m.Data[folderID]; !found {
		return rest.ErrNotFound
	}
	m.Added[folderID] = append(m.Added[folderID], cardIDs...)
	return nil
}

func (m *MockMusicCardFolderRepo) RemoveCard(folderID, cardID string) error {
	if m.Err {
		return errors.New("error")
	}
	if m.Denied[folderID] {
		return rest.ErrPermissionDenied
	}
	if _, found := m.Data[folderID]; !found {
		return rest.ErrNotFound
	}
	m.Added[folderID] = slices.DeleteFunc(m.Added[folderID], func(c string) bool { return c == cardID })
	return nil
}

func (m *MockMusicCardFolderRepo) Cards(folderID string) (model.MusicCardsWithSnippets, error) {
	if m.Err {
		return nil, errors.New("error")
	}
	if cards, ok := m.CardsData[folderID]; ok {
		return cards, nil
	}
	return nil, model.ErrNotFound
}

func (m *MockMusicCardFolderRepo) EntityName() string {
	return "music_card_folder"
}

func (m *MockMusicCardFolderRepo) NewInstance() any {
	return &model.MusicCardFolder{ReviewEnabled: true}
}

func (m *MockMusicCardFolderRepo) Read(id string) (any, error) {
	f, err := m.Get(id)
	if errors.Is(err, model.ErrNotFound) {
		return nil, rest.ErrNotFound
	}
	return f, err
}

func (m *MockMusicCardFolderRepo) ReadAll(options ...rest.QueryOptions) (any, error) {
	return m.GetAll()
}

func (m *MockMusicCardFolderRepo) Save(entity any) (string, error) {
	f := entity.(*model.MusicCardFolder)
	if err := m.Put(f); err != nil {
		return "", err
	}
	return f.ID, nil
}

func (m *MockMusicCardFolderRepo) Update(id string, entity any, cols ...string) error {
	current, found := m.Data[id]
	if !found {
		return rest.ErrNotFound
	}
	if m.Denied[id] {
		return rest.ErrPermissionDenied
	}
	f := entity.(*model.MusicCardFolder)
	if err := m.checkNameFree(id, f.Name); err != nil {
		return err
	}
	f.ID = id
	f.UserID = current.UserID // ownership is immutable
	f.IsDefault = current.IsDefault
	m.Data[id] = f
	return nil
}

// checkNameFree mirrors the (user_id, name) unique constraint the real repository maps to a
// validation error.
func (m *MockMusicCardFolderRepo) checkNameFree(id, name string) error {
	for otherID, other := range m.Data {
		if otherID != id && other.Name == name {
			return &rest.ValidationError{Errors: map[string]string{"name": "ra.validation.unique"}}
		}
	}
	return nil
}

var _ model.MusicCardFolderRepository = (*MockMusicCardFolderRepo)(nil)
