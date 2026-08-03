package project

import (
	"fmt"

	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"

	"github.com/safedep/cli/internal/tui"
)

const (
	// The ListScans filter caps repeated project and version selectors at ten
	// items each.
	maxScanFilterValues = 10

	scanStatusPrefix  = "SCAN_STATUS_"
	scanTriggerPrefix = "SCAN_TRIGGER_"
)

func scanStatusTokens() []string {
	return tui.EnumTokens(messagescontroltowerv1.ScanStatus_name, scanStatusPrefix)
}

func scanTriggerTokens() []string {
	return tui.EnumTokens(messagescontroltowerv1.ScanTrigger_name, scanTriggerPrefix)
}

func validateListInput(in *listInput) error {
	if err := validateScanFilterValues(in.Projects, "project name"); err != nil {
		return err
	}
	if err := validateScanFilterValues(in.ProjectVersions, "project version"); err != nil {
		return err
	}
	if _, err := parseScanStatus(in.Status); err != nil {
		return err
	}
	_, err := parseScanTrigger(in.Trigger)
	return err
}

func validateScanFilterValues(values []string, label string) error {
	if len(values) > maxScanFilterValues {
		cause := fmt.Errorf("project scan list accepts at most %d %s filters", maxScanFilterValues, label)
		return invalidProjectSelectionError(
			cause,
			fmt.Sprintf("Provide at most %d %s filters.", maxScanFilterValues, label),
		)
	}
	return validateUniqueValues(values, label)
}

func parseScanStatus(token string) (messagescontroltowerv1.ScanStatus, error) {
	unspecified := messagescontroltowerv1.ScanStatus_SCAN_STATUS_UNSPECIFIED
	if token == "" {
		return unspecified, nil
	}
	number, ok := tui.ParseEnumToken(messagescontroltowerv1.ScanStatus_name, scanStatusPrefix, token)
	if !ok {
		return unspecified, unknownFilterValueError("status", token, scanStatusTokens())
	}
	return messagescontroltowerv1.ScanStatus(number), nil
}

func parseScanTrigger(token string) (messagescontroltowerv1.ScanTrigger, error) {
	unspecified := messagescontroltowerv1.ScanTrigger_SCAN_TRIGGER_UNSPECIFIED
	if token == "" {
		return unspecified, nil
	}
	number, ok := tui.ParseEnumToken(messagescontroltowerv1.ScanTrigger_name, scanTriggerPrefix, token)
	if !ok {
		return unspecified, unknownFilterValueError("trigger", token, scanTriggerTokens())
	}
	return messagescontroltowerv1.ScanTrigger(number), nil
}
