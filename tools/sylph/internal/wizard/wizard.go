package wizard

import (
	"errors"
	"fmt"
	"strings"

	"charm.land/huh/v2"

	"github.com/ITG-Labs/sylph/tools/sylph/internal/envfile"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/role"
)

var ErrAborted = errors.New("отменено")

type Wizard struct {
	Run func(*huh.Form) error
}

func (w Wizard) run(groups ...*huh.Group) error {
	form := huh.NewForm(groups...).WithTheme(huh.ThemeFunc(huh.ThemeCharm)).WithShowHelp(true)
	var err error
	if w.Run != nil {
		err = w.Run(form)
	} else {
		err = form.Run()
	}
	if errors.Is(err, huh.ErrUserAborted) {
		return ErrAborted
	}
	return err
}

func (w Wizard) ChooseRole() (*role.Role, error) {
	var name string
	var opts []huh.Option[string]
	for _, r := range role.All() {
		opts = append(opts, huh.NewOption(r.Title, r.Name))
	}
	if err := w.run(huh.NewGroup(huh.NewSelect[string]().Title("Какую роль ставим на эту машину?").Options(opts...).Value(&name))); err != nil {
		return nil, err
	}
	return role.Get(name)
}

func (w Wizard) Select(title string, labels []string) (int, error) {
	var idx int
	var opts []huh.Option[int]
	for i, l := range labels {
		opts = append(opts, huh.NewOption(l, i))
	}
	err := w.run(huh.NewGroup(huh.NewSelect[int]().Title(title).Options(opts...).Value(&idx)))
	return idx, err
}

func (w Wizard) Confirm(title string, description string, def bool) (bool, error) {
	v := def
	err := w.run(huh.NewGroup(huh.NewConfirm().Title(title).Description(description).Affirmative("Да").Negative("Нет").Value(&v)))
	return v, err
}

func (w Wizard) Input(title string, description string, validate func(string) error) (string, error) {
	var v string
	err := w.run(huh.NewGroup(huh.NewInput().Title(title).Description(description).Value(&v).Validate(validate)))
	return strings.TrimSpace(v), err
}

func (w Wizard) Pause(title string, description string) error {
	return w.run(huh.NewGroup(huh.NewNote().Title(title).Description(description).Next(true).NextLabel("Записал")))
}

type Request struct {
	Role           *role.Role
	Env            *envfile.Env
	Examples       *envfile.Env
	FirstInstall   bool
	CertsUntil     string
	CheckBundle    func(string) error
	ExistingAdmins []string
}

type Answers struct {
	Env       *envfile.Env
	Bundle    string
	NewAdmins []string
}

func (w Wizard) Ask(req Request) (*Answers, error) {
	r := req.Role
	values := map[string]*string{}
	bools := map[string]*bool{}
	toggles := map[string]*bool{}
	for _, t := range r.Toggles {
		on := r.ToggleOn(t.Name, req.Env)
		toggles[t.Name] = &on
	}
	advanced := false

	var main, adv []huh.Field
	toggleFields := map[string][]huh.Field{}
	for _, f := range r.Fields {
		field := w.field(f, req, values, bools)
		switch {
		case f.Toggle != "":
			toggleFields[f.Toggle] = append(toggleFields[f.Toggle], field)
		case f.Advanced:
			adv = append(adv, field)
		default:
			main = append(main, field)
		}
	}

	var groups []*huh.Group
	if len(main) > 0 {
		groups = append(groups, huh.NewGroup(main...).Title(r.Title))
	}
	for _, t := range r.Toggles {
		on := toggles[t.Name]
		groups = append(groups,
			huh.NewGroup(huh.NewConfirm().Title(t.Title).Description(t.Description).Affirmative("Да").Negative("Нет").Value(on)),
			huh.NewGroup(toggleFields[t.Name]...).WithHideFunc(func() bool { return !*on }),
		)
	}

	adminsRaw := ""
	addAdmins := len(req.ExistingAdmins) == 0
	if r.Admins {
		if len(req.ExistingAdmins) > 0 {
			desc := "уже есть: " + strings.Join(req.ExistingAdmins, ", ")
			groups = append(groups, huh.NewGroup(huh.NewConfirm().Title("Добавить админа панели?").Description(desc).Affirmative("Да").Negative("Нет").Value(&addAdmins)))
		}
		groups = append(groups, huh.NewGroup(huh.NewInput().Title("Имена новых админов").Description("через запятую; для каждого покажу пароль и QR для TOTP").Value(&adminsRaw).Validate(func(s string) error {
			names, err := role.AdminNames(s)
			if err != nil {
				return err
			}
			if len(names) == 0 {
				return errors.New("нужен хотя бы один админ")
			}
			for _, n := range names {
				for _, e := range req.ExistingAdmins {
					if n == e {
						return fmt.Errorf("админ %q уже есть", n)
					}
				}
			}
			return nil
		})).WithHideFunc(func() bool { return !addAdmins }))
	}

	bundleInput := ""
	newCerts := req.CertsUntil == ""
	if !newCerts {
		groups = append(groups, huh.NewGroup(huh.NewSelect[bool]().Title("Серты роли").Options(
			huh.NewOption("Оставить текущие (действуют до "+req.CertsUntil+")", false),
			huh.NewOption("Поставить новый пакет", true),
		).Value(&newCerts)))
	}
	groups = append(groups, huh.NewGroup(huh.NewInput().
		Title("Пакет сертов").
		Description("путь к файлу .sylph-certs или строка sylph-certs:… из `sylph certs bundle " + r.Name + "` на ноуте").
		CharLimit(1<<16).
		Value(&bundleInput).
		Validate(req.CheckBundle)).WithHideFunc(func() bool { return !newCerts }))

	if len(adv) > 0 {
		groups = append(groups,
			huh.NewGroup(huh.NewConfirm().Title("Дополнительные настройки?").Description("порт, подсеть, имя в серте ядра, логи — обычно не нужно").Affirmative("Да").Negative("Нет").Value(&advanced)),
			huh.NewGroup(adv...).Title("Дополнительно").WithHideFunc(func() bool { return !advanced }),
		)
	}

	if err := w.run(groups...); err != nil {
		return nil, err
	}

	env := req.Env.Clone()
	for _, f := range r.Fields {
		if f.Toggle != "" && !*toggles[f.Toggle] {
			env.Set(f.Key, "")
			continue
		}
		if b, ok := bools[f.Key]; ok {
			env.Set(f.Key, fmt.Sprint(*b))
			continue
		}
		env.Set(f.Key, strings.TrimSpace(*values[f.Key]))
	}
	ans := &Answers{Env: env}
	if newCerts {
		ans.Bundle = strings.TrimSpace(bundleInput)
	}
	if r.Admins && addAdmins {
		names, err := role.AdminNames(adminsRaw)
		if err != nil {
			return nil, err
		}
		ans.NewAdmins = names
	}
	return ans, nil
}

func (w Wizard) field(f role.Field, req Request, values map[string]*string, bools map[string]*bool) huh.Field {
	cur := req.Env.Get(f.Key)
	switch f.Kind {
	case role.Bool:
		b := cur == "true"
		bools[f.Key] = &b
		return huh.NewConfirm().Title(f.Title).Description(f.Description).Affirmative("Да").Negative("Нет").Value(&b)
	case role.Choice:
		v := cur
		values[f.Key] = &v
		return huh.NewSelect[string]().Title(f.Title).Description(f.Description).Options(huh.NewOptions(f.Choices...)...).Value(&v)
	}
	v := cur
	values[f.Key] = &v
	in := huh.NewInput().Title(f.Title).Description(f.Description).Value(&v).Validate(func(s string) error {
		s = strings.TrimSpace(s)
		if err := envfile.ValidValue(s); err != nil {
			return err
		}
		if s == "" {
			if f.Optional {
				return nil
			}
			return errors.New("нужно заполнить")
		}
		return f.Check(s)
	})
	if f.Example && req.Examples != nil {
		in = in.Placeholder(req.Examples.Get(f.Key))
	}
	if f.Kind == role.Secret {
		in = in.EchoMode(huh.EchoModePassword)
	}
	return in
}
