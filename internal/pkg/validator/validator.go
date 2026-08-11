package validator

import (
	"errors"
	"fmt"
	"strings"

	"github.com/go-playground/locales/en"
	ut "github.com/go-playground/universal-translator"
	"github.com/go-playground/validator/v10"
	enTranslations "github.com/go-playground/validator/v10/translations/en"
)

type Validator struct {
	validator *validator.Validate
	trans     ut.Translator
}

func New() (*Validator, error) {
	validate := validator.New()
	en := en.New()
	uni := ut.New(en, en)
	trans, ok := uni.GetTranslator("en")
	if !ok {
		return nil, errors.New("load English translator")
	}
	if err := enTranslations.RegisterDefaultTranslations(validate, trans); err != nil {
		return nil, fmt.Errorf("register English translations: %w", err)
	}
	return &Validator{validator: validate, trans: trans}, nil
}

func (v *Validator) Validate(i any) error {
	if err := v.validator.Struct(i); err != nil {
		if validationErrors, ok := err.(validator.ValidationErrors); ok {
			errs := make([]string, 0, len(validationErrors))
			for _, e := range validationErrors {
				errs = append(errs, e.Translate(v.trans))
			}
			if len(errs) == 0 {
				return err
			}
			return errors.New(strings.Join(errs, ", "))
		}
		return fmt.Errorf("validate input: %w", err)
	}
	return nil
}
