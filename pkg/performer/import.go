package performer

import (
	"context"
	"fmt"
	"strings"

	"github.com/stashapp/stash/pkg/hash/md5"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/models/jsonschema"
	"github.com/stashapp/stash/pkg/sliceutil/stringslice"
	"github.com/stashapp/stash/pkg/tag"
	"github.com/stashapp/stash/pkg/utils"
)

type NameFinderCreatorUpdater interface {
	NameFinderCreator
	Update(ctx context.Context, updatedPerformer *models.Performer) error
	UpdateTags(ctx context.Context, performerID int, tagIDs []int) error
	UpdateImage(ctx context.Context, performerID int, image []byte) error
	UpdateStashIDs(ctx context.Context, performerID int, stashIDs []models.StashID) error
}

type Importer struct {
	ReaderWriter        NameFinderCreatorUpdater
	TagWriter           tag.NameFinderCreator
	Input               jsonschema.Performer
	MissingRefBehaviour models.ImportMissingRefEnum

	ID        int
	performer models.Performer
	imageData []byte

	tags []*models.Tag
}

func (i *Importer) PreImport(ctx context.Context) error {
	i.performer = performerJSONToPerformer(i.Input)

	if err := i.populateTags(ctx); err != nil {
		return err
	}

	var err error
	if len(i.Input.Image) > 0 {
		i.imageData, err = utils.ProcessBase64Image(i.Input.Image)
		if err != nil {
			return fmt.Errorf("invalid image: %v", err)
		}
	}

	return nil
}

func (i *Importer) populateTags(ctx context.Context) error {
	if len(i.Input.Tags) > 0 {

		tags, err := importTags(ctx, i.TagWriter, i.Input.Tags, i.MissingRefBehaviour)
		if err != nil {
			return err
		}

		i.tags = tags
	}

	return nil
}

func importTags(ctx context.Context, tagWriter tag.NameFinderCreator, names []string, missingRefBehaviour models.ImportMissingRefEnum) ([]*models.Tag, error) {
	tags, err := tagWriter.FindByNames(ctx, names, false)
	if err != nil {
		return nil, err
	}

	var pluckedNames []string
	for _, tag := range tags {
		pluckedNames = append(pluckedNames, tag.Name)
	}

	missingTags := stringslice.StrFilter(names, func(name string) bool {
		return !stringslice.StrInclude(pluckedNames, name)
	})

	if len(missingTags) > 0 {
		if missingRefBehaviour == models.ImportMissingRefEnumFail {
			return nil, fmt.Errorf("tags [%s] not found", strings.Join(missingTags, ", "))
		}

		if missingRefBehaviour == models.ImportMissingRefEnumCreate {
			createdTags, err := createTags(ctx, tagWriter, missingTags)
			if err != nil {
				return nil, fmt.Errorf("error creating tags: %v", err)
			}

			tags = append(tags, createdTags...)
		}

		// ignore if MissingRefBehaviour set to Ignore
	}

	return tags, nil
}

func createTags(ctx context.Context, tagWriter tag.NameFinderCreator, names []string) ([]*models.Tag, error) {
	var ret []*models.Tag
	for _, name := range names {
		newTag := *models.NewTag(name)

		created, err := tagWriter.Create(ctx, newTag)
		if err != nil {
			return nil, err
		}

		ret = append(ret, created)
	}

	return ret, nil
}

func (i *Importer) PostImport(ctx context.Context, id int) error {
	if len(i.tags) > 0 {
		var tagIDs []int
		for _, t := range i.tags {
			tagIDs = append(tagIDs, t.ID)
		}
		if err := i.ReaderWriter.UpdateTags(ctx, id, tagIDs); err != nil {
			return fmt.Errorf("failed to associate tags: %v", err)
		}
	}

	if len(i.imageData) > 0 {
		if err := i.ReaderWriter.UpdateImage(ctx, id, i.imageData); err != nil {
			return fmt.Errorf("error setting performer image: %v", err)
		}
	}

	if len(i.Input.StashIDs) > 0 {
		if err := i.ReaderWriter.UpdateStashIDs(ctx, id, i.Input.StashIDs); err != nil {
			return fmt.Errorf("error setting stash id: %v", err)
		}
	}

	return nil
}

func (i *Importer) Name() string {
	return i.Input.Name
}

func (i *Importer) FindExistingID(ctx context.Context) (*int, error) {
	const nocase = false
	existing, err := i.ReaderWriter.FindByNames(ctx, []string{i.Name()}, nocase)
	if err != nil {
		return nil, err
	}

	if len(existing) > 0 {
		id := existing[0].ID
		return &id, nil
	}

	return nil, nil
}

func (i *Importer) Create(ctx context.Context) (*int, error) {
	err := i.ReaderWriter.Create(ctx, &i.performer)
	if err != nil {
		return nil, fmt.Errorf("error creating performer: %v", err)
	}

	id := i.performer.ID
	return &id, nil
}

func (i *Importer) Update(ctx context.Context, id int) error {
	performer := i.performer
	performer.ID = id
	err := i.ReaderWriter.Update(ctx, &performer)
	if err != nil {
		return fmt.Errorf("error updating existing performer: %v", err)
	}

	return nil
}

func performerJSONToPerformer(performerJSON jsonschema.Performer) models.Performer {
	checksum := md5.FromString(performerJSON.Name)

	newPerformer := models.Performer{
		Name:          performerJSON.Name,
		Checksum:      checksum,
		Gender:        models.GenderEnum(performerJSON.Gender),
		URL:           performerJSON.URL,
		Ethnicity:     performerJSON.Ethnicity,
		Country:       performerJSON.Country,
		EyeColor:      performerJSON.EyeColor,
		Height:        performerJSON.Height,
		Measurements:  performerJSON.Measurements,
		FakeTits:      performerJSON.FakeTits,
		CareerLength:  performerJSON.CareerLength,
		Tattoos:       performerJSON.Tattoos,
		Piercings:     performerJSON.Piercings,
		Aliases:       performerJSON.Aliases,
		Twitter:       performerJSON.Twitter,
		Instagram:     performerJSON.Instagram,
		Details:       performerJSON.Details,
		HairColor:     performerJSON.HairColor,
		Favorite:      performerJSON.Favorite,
		IgnoreAutoTag: performerJSON.IgnoreAutoTag,
		CreatedAt:     performerJSON.CreatedAt.GetTime(),
		UpdatedAt:     performerJSON.UpdatedAt.GetTime(),
	}

	if performerJSON.Birthdate != "" {
		d, err := utils.ParseDateStringAsTime(performerJSON.Birthdate)
		if err == nil {
			newPerformer.Birthdate = &models.Date{
				Time: d,
			}
		}
	}
	if performerJSON.Rating != 0 {
		newPerformer.Rating = &performerJSON.Rating
	}
	if performerJSON.DeathDate != "" {
		d, err := utils.ParseDateStringAsTime(performerJSON.DeathDate)
		if err == nil {
			newPerformer.DeathDate = &models.Date{
				Time: d,
			}
		}
	}

	if performerJSON.Weight != 0 {
		newPerformer.Weight = &performerJSON.Weight
	}

	return newPerformer
}
