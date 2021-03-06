package deprecatetemplates

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/test-infra/prow/config"
	"k8s.io/test-infra/prow/plugins"
)

func TestLoadTemplates(t *testing.T) {
	testcases := []struct {
		description string
		updaterCfg  plugins.ConfigUpdater
		expected    sets.String
	}{
		{
			description: "template is detected",
			updaterCfg: plugins.ConfigUpdater{
				Maps: map[string]plugins.ConfigMapSpec{
					"ci-operator/templates/this-is-a-template.yaml": {Name: "template"},
				},
			},
			expected: sets.NewString("template"),
		},
		{
			description: "not a template is ignored",
			updaterCfg: plugins.ConfigUpdater{
				Maps: map[string]plugins.ConfigMapSpec{
					"ci-operator/config/this/is-not/a-template.yaml": {Name: "not-a-template"},
				},
			},
			expected: sets.NewString(),
		},
	}

	for _, tc := range testcases {
		t.Run(tc.description, func(t *testing.T) {
			enforcer := Enforcer{}
			enforcer.LoadTemplates(tc.updaterCfg)
			if diff := cmp.Diff(tc.expected, enforcer.existingTemplates); diff != "" {
				t.Errorf("%s: templates differ from expected:\n%s", tc.description, diff)
			}
		})
	}
}

type mockAllowlist struct {
	jobs         map[string]sets.String
	getTemplates map[string]*deprecatedTemplate
}

func (m *mockAllowlist) Insert(job config.JobBase, template string) {
	if _, ok := m.jobs[template]; !ok {
		m.jobs[template] = sets.NewString()
	}
	m.jobs[template].Insert(job.Name)
}

func (m *mockAllowlist) Save(_ string) error {
	panic("this should never be called")
}

func (m *mockAllowlist) Prune() {
	panic("this should never be called")
}

func (m *mockAllowlist) GetTemplates() map[string]*deprecatedTemplate {
	return m.getTemplates
}

type mockJobConfig struct {
	presubmits  []config.Presubmit
	postsubmits []config.Postsubmit
	periodics   []config.Periodic
}

func (m *mockJobConfig) AllStaticPostsubmits(_ []string) []config.Postsubmit {
	return append([]config.Postsubmit{}, m.postsubmits...)
}
func (m *mockJobConfig) AllStaticPresubmits(_ []string) []config.Presubmit {
	return append([]config.Presubmit{}, m.presubmits...)
}
func (m *mockJobConfig) AllPeriodics() []config.Periodic {
	return append([]config.Periodic{}, m.periodics...)
}

func cmVolume(name, cmName string) corev1.Volume {
	return corev1.Volume{
		Name: name,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: cmName},
			},
		},
	}
}

func TestProcessJobs(t *testing.T) {
	template := "template"
	jobWithTemplate := config.JobBase{
		Name: "job-with-template",
		Spec: &corev1.PodSpec{
			Volumes: []corev1.Volume{cmVolume("volume", template)},
		},
	}
	jobWithoutTemplate := config.JobBase{
		Name: "job-without-template",
		Spec: &corev1.PodSpec{},
	}

	testcases := []struct {
		description string
		presubmits  []config.Presubmit
		postsubmits []config.Postsubmit
		periodics   []config.Periodic

		inserted sets.String
	}{
		{
			description: "presubmit using template is added",
			presubmits:  []config.Presubmit{{JobBase: jobWithTemplate}},
			inserted:    sets.NewString("job-with-template"),
		},
		{
			description: "postsubmit using template is added",
			postsubmits: []config.Postsubmit{{JobBase: jobWithTemplate}},
			inserted:    sets.NewString("job-with-template"),
		},
		{
			description: "periodics using template is added",
			periodics:   []config.Periodic{{JobBase: jobWithTemplate}},
			inserted:    sets.NewString("job-with-template"),
		},
		{
			description: "jobs not using template are ignored",
			presubmits:  []config.Presubmit{{JobBase: jobWithTemplate}, {JobBase: jobWithoutTemplate}},
			postsubmits: []config.Postsubmit{{JobBase: jobWithoutTemplate}},
			periodics:   []config.Periodic{{JobBase: jobWithoutTemplate}},
			inserted:    sets.NewString("job-with-template"),
		},
	}

	for _, tc := range testcases {
		mock := mockAllowlist{jobs: map[string]sets.String{}}
		mockJobs := &mockJobConfig{
			presubmits:  tc.presubmits,
			postsubmits: tc.postsubmits,
			periodics:   tc.periodics,
		}
		t.Run(tc.description, func(t *testing.T) {
			enforcer := Enforcer{
				existingTemplates: sets.NewString(template),
				allowlist:         &mock,
			}
			enforcer.ProcessJobs(mockJobs)

			if jobs, ok := mock.jobs[template]; !ok {
				t.Errorf("%s: no record added for template '%s'", tc.description, template)
			} else if diff := cmp.Diff(jobs, tc.inserted); diff != "" {
				t.Errorf("%s: inserted jobs differ:\n%s", tc.description, diff)
			}
		})
	}
}

func TestEnforcerStats(t *testing.T) {
	mock := &mockAllowlist{
		getTemplates: map[string]*deprecatedTemplate{
			"template-1": {
				Name: "template-1",
				UnknownBlocker: deprecatedTemplateBlocker{
					Jobs: blockedJobs{
						"1": blockedJob{Generated: false, Kind: "periodic"},
						"2": blockedJob{Generated: false, Kind: "periodic"},
						"3": blockedJob{Generated: false, Kind: "periodic"},
						"4": blockedJob{Generated: false, Kind: "periodic"},
						"5": blockedJob{Generated: false, Kind: "periodic"},
					},
				},
			},
			"template-2": {
				Name: "template-2",
				Blockers: map[string]deprecatedTemplateBlocker{
					"B1": {Jobs: blockedJobs{"6": blockedJob{Generated: true, Kind: "presubmit"}}},
					"B2": {Jobs: blockedJobs{"7": blockedJob{Generated: true, Kind: "postsubmit"}}},
				},
			},
			"template-3": {
				Name: "template-3",
				UnknownBlocker: deprecatedTemplateBlocker{
					Jobs: blockedJobs{"8": blockedJob{Generated: false, Kind: "periodic"}},
				},
				Blockers: map[string]deprecatedTemplateBlocker{
					"B3": {Jobs: blockedJobs{"9": blockedJob{Generated: true, Kind: "presubmit"}}},
					"B4": {Jobs: blockedJobs{"10": blockedJob{Generated: true, Kind: "presubmit"}}},
				},
			},
		},
	}
	enforcer := &Enforcer{allowlist: mock}
	expectedHeader := []string{"Template", "Blocker", "Total", "Generated", "Handcrafted", "Presubmits", "Postsubmits", "Release", "Periodics", "Unknown"}
	expectedFooter := []string{"3 templates", "Total", "10", "4", "6", "3", "1", "0", "6", "0"}
	expectedData := [][]string{
		{"template-2", blockerColUnknown, "0", "0", "0", "0", "0", "0", "0", "0"},
		{"template-2", "B1", "1", "1", "0", "1", "0", "0", "0", "0"},
		{"template-2", "B2", "1", "1", "0", "0", "1", "0", "0", "0"},
		{"template-2", blockerColTotal, "2", "2", "0", "1", "1", "0", "0", "0"},
		{"template-3", "B3", "1", "1", "0", "1", "0", "0", "0", "0"},
		{"template-3", "B4", "1", "1", "0", "1", "0", "0", "0", "0"},
		{"template-3", blockerColUnknown, "1", "0", "1", "0", "0", "0", "1", "0"},
		{"template-3", blockerColTotal, "3", "2", "1", "2", "0", "0", "1", "0"},
		{"template-1", blockerColUnknown, "5", "0", "5", "0", "0", "0", "5", "0"},
		{"template-1", blockerColTotal, "5", "0", "5", "0", "0", "0", "5", "0"},
	}

	header, footer, data := enforcer.Stats()
	if diff := cmp.Diff(expectedHeader, header); diff != "" {
		t.Errorf("Header differs from expected:\n%s", diff)
	}
	if diff := cmp.Diff(expectedFooter, footer); diff != "" {
		t.Errorf("Footer differs from expected:\n%s", diff)
	}
	if diff := cmp.Diff(expectedData, data); diff != "" {
		t.Errorf("Data differs from expected:\n%s", diff)
	}
}
