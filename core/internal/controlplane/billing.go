package controlplane

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	stripeSignatureTolerance = 5 * time.Minute
	defaultStripeAPIBaseURL  = "https://api.stripe.com"
)

type StripeConfig struct {
	APIBaseURL         string
	PublishableKey     string
	SecretKey          string
	WebhookSecret      string
	CheckoutSuccessURL string
	CheckoutCancelURL  string
	PortalReturnURL    string
	PlanPriceIDs       map[string]string
}

func normalizeStripeConfig(config StripeConfig) StripeConfig {
	normalized := StripeConfig{
		APIBaseURL:         strings.TrimSpace(config.APIBaseURL),
		PublishableKey:     strings.TrimSpace(config.PublishableKey),
		SecretKey:          strings.TrimSpace(config.SecretKey),
		WebhookSecret:      strings.TrimSpace(config.WebhookSecret),
		CheckoutSuccessURL: strings.TrimSpace(config.CheckoutSuccessURL),
		CheckoutCancelURL:  strings.TrimSpace(config.CheckoutCancelURL),
		PortalReturnURL:    strings.TrimSpace(config.PortalReturnURL),
		PlanPriceIDs:       map[string]string{},
	}
	for plan, priceID := range config.PlanPriceIDs {
		trimmedPlan := strings.TrimSpace(plan)
		trimmedPriceID := strings.TrimSpace(priceID)
		if trimmedPlan == "" || trimmedPriceID == "" {
			continue
		}
		normalized.PlanPriceIDs[trimmedPlan] = trimmedPriceID
	}
	if normalized.APIBaseURL == "" {
		normalized.APIBaseURL = defaultStripeAPIBaseURL
	}
	normalized.APIBaseURL = strings.TrimRight(normalized.APIBaseURL, "/")
	return normalized
}

func insertOrganizationBillingTx(ctx context.Context, tx *sql.Tx, organizationID string, nowText string) error {
	if _, err := tx.ExecContext(
		ctx,
		`INSERT OR IGNORE INTO organization_billing(
			organization_id, provider, billing_status, stripe_customer_id, stripe_subscription_id, stripe_price_id,
			stripe_subscription_status, current_period_end, cancel_at_period_end, last_webhook_event_id,
			last_webhook_event_type, last_webhook_received_at, created_at, updated_at
		) VALUES (?, 'stripe', 'free', '', '', '', 'not_started', NULL, 0, '', '', NULL, ?, ?)`,
		organizationID,
		nowText,
		nowText,
	); err != nil {
		return internalError("failed to initialize organization billing")
	}
	return nil
}

func (s *Service) GetBillingSummary(ctx context.Context, identity RequestIdentity, organizationID string) (BillingSummary, error) {
	organization, _, err := s.requireOrganizationAccess(ctx, identity, organizationID, false)
	if err != nil {
		return BillingSummary{}, err
	}
	usageSummary, err := s.getUsageSummaryForOrganization(ctx, organization.ID, organization.PlanTier)
	if err != nil {
		return BillingSummary{}, err
	}
	account, err := s.ensureOrganizationBilling(ctx, organization.ID)
	if err != nil {
		return BillingSummary{}, err
	}
	return BillingSummary{
		OrganizationID: organization.ID,
		PlanTier:       organization.PlanTier,
		BillingAccount: account,
		UsageSummary:   usageSummary,
		Configuration:  s.billingConfiguration(),
	}, nil
}

func (s *Service) CreateBillingCheckoutSession(ctx context.Context, identity RequestIdentity, organizationID string, planTier string) (BillingActionSession, error) {
	organization, membership, err := s.requireOrganizationAccess(ctx, identity, organizationID, true)
	if err != nil {
		return BillingActionSession{}, err
	}
	if !membershipCanManage(membership.Role) {
		return BillingActionSession{}, accessDenied("billing checkout requires owner or admin access")
	}
	planTier = strings.TrimSpace(planTier)
	if planTier == "" {
		planTier = organization.PlanTier
	}
	if err := validatePlanTier(planTier); err != nil {
		return BillingActionSession{}, err
	}
	if planTier == "starter" {
		return BillingActionSession{}, invalidRequest("starter plan does not require Stripe checkout")
	}

	account, err := s.ensureOrganizationBilling(ctx, organization.ID)
	if err != nil {
		return BillingActionSession{}, err
	}
	missing := s.checkoutMissingConfiguration(planTier)
	if len(missing) > 0 {
		return BillingActionSession{
			Provider:             "stripe",
			Mode:                 "checkout",
			Status:               "configuration_required",
			PlanTier:             planTier,
			MissingConfiguration: missing,
			Note:                 "Fill the Stripe env values first.",
		}, nil
	}
	if stripeSubscriptionManaged(account) {
		return BillingActionSession{}, invalidRequest("organization already has a Stripe subscription; use the customer portal to manage plan changes")
	}

	customerID := strings.TrimSpace(account.StripeCustomerID)
	if customerID == "" {
		customerID, err = s.ensureStripeCustomer(ctx, organization, identity.Account, account)
		if err != nil {
			return BillingActionSession{}, err
		}
	}
	sessionURL, err := s.createStripeCheckoutSession(ctx, organization, planTier, customerID)
	if err != nil {
		return BillingActionSession{}, err
	}
	return BillingActionSession{
		Provider: "stripe",
		Mode:     "checkout",
		Status:   "created",
		PlanTier: planTier,
		URL:      sessionURL,
	}, nil
}

func (s *Service) CreateBillingCustomerPortalSession(ctx context.Context, identity RequestIdentity, organizationID string) (BillingActionSession, error) {
	_, membership, err := s.requireOrganizationAccess(ctx, identity, organizationID, true)
	if err != nil {
		return BillingActionSession{}, err
	}
	if !membershipCanManage(membership.Role) {
		return BillingActionSession{}, accessDenied("billing portal requires owner or admin access")
	}
	account, err := s.ensureOrganizationBilling(ctx, organizationID)
	if err != nil {
		return BillingActionSession{}, err
	}
	missing := s.customerPortalMissingConfiguration(account)
	if len(missing) > 0 {
		return BillingActionSession{
			Provider:             "stripe",
			Mode:                 "customer_portal",
			Status:               "configuration_required",
			MissingConfiguration: missing,
			Note:                 "Fill the Stripe env values and customer linkage first.",
		}, nil
	}
	sessionURL, err := s.createStripeCustomerPortalSession(ctx, account.StripeCustomerID)
	if err != nil {
		return BillingActionSession{}, err
	}
	return BillingActionSession{
		Provider: "stripe",
		Mode:     "customer_portal",
		Status:   "created",
		URL:      sessionURL,
	}, nil
}

func stripeSubscriptionManaged(account BillingAccount) bool {
	if strings.TrimSpace(account.StripeSubscriptionID) == "" {
		return false
	}
	switch strings.TrimSpace(account.StripeSubscriptionStatus) {
	case "", "free", "not_started", "canceled", "incomplete_expired", "unpaid":
		return false
	default:
		return true
	}
}

func (s *Service) ensureStripeCustomer(ctx context.Context, organization Organization, account Account, billing BillingAccount) (string, error) {
	if customerID := strings.TrimSpace(billing.StripeCustomerID); customerID != "" {
		return customerID, nil
	}

	form := url.Values{}
	form.Set("name", organization.DisplayName)
	form.Set("email", account.Email)
	form.Set("metadata[organization_id]", organization.ID)
	form.Set("metadata[organization_slug]", organization.Slug)
	form.Set("metadata[account_id]", account.ID)

	var response struct {
		ID string `json:"id"`
	}
	if err := s.stripeAPIPostForm(ctx, "/v1/customers", form, &response); err != nil {
		return "", err
	}
	if strings.TrimSpace(response.ID) == "" {
		return "", internalError("stripe customer response did not include an id")
	}
	if err := s.updateOrganizationBillingCustomer(ctx, organization.ID, response.ID); err != nil {
		return "", err
	}
	return response.ID, nil
}

func (s *Service) createStripeCheckoutSession(ctx context.Context, organization Organization, planTier string, customerID string) (string, error) {
	form := url.Values{}
	form.Set("mode", "subscription")
	form.Set("success_url", s.stripe.CheckoutSuccessURL)
	form.Set("cancel_url", s.stripe.CheckoutCancelURL)
	form.Set("client_reference_id", organization.ID)
	form.Set("customer", customerID)
	form.Set("line_items[0][price]", s.stripe.PlanPriceIDs[planTier])
	form.Set("line_items[0][quantity]", "1")
	form.Set("metadata[organization_id]", organization.ID)
	form.Set("metadata[plan_tier]", planTier)
	form.Set("subscription_data[metadata][organization_id]", organization.ID)
	form.Set("subscription_data[metadata][plan_tier]", planTier)

	var response struct {
		URL string `json:"url"`
	}
	if err := s.stripeAPIPostForm(ctx, "/v1/checkout/sessions", form, &response); err != nil {
		return "", err
	}
	if strings.TrimSpace(response.URL) == "" {
		return "", internalError("stripe checkout session response did not include a url")
	}
	return response.URL, nil
}

func (s *Service) createStripeCustomerPortalSession(ctx context.Context, customerID string) (string, error) {
	form := url.Values{}
	form.Set("customer", customerID)
	form.Set("return_url", s.stripe.PortalReturnURL)

	var response struct {
		URL string `json:"url"`
	}
	if err := s.stripeAPIPostForm(ctx, "/v1/billing_portal/sessions", form, &response); err != nil {
		return "", err
	}
	if strings.TrimSpace(response.URL) == "" {
		return "", internalError("stripe customer portal response did not include a url")
	}
	return response.URL, nil
}

func (s *Service) stripeAPIPostForm(ctx context.Context, path string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.stripe.APIBaseURL+path, strings.NewReader(form.Encode()))
	if err != nil {
		return internalError("failed to create stripe request")
	}
	req.SetBasicAuth(s.stripe.SecretKey, "")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return internalError("failed to reach stripe")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return internalError("failed to read stripe response")
	}
	if resp.StatusCode >= 400 {
		var stripeErr struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(body, &stripeErr); err == nil && strings.TrimSpace(stripeErr.Error.Message) != "" {
			return invalidRequest("stripe request failed: " + strings.TrimSpace(stripeErr.Error.Message))
		}
		return internalError(fmt.Sprintf("stripe request failed with status %d", resp.StatusCode))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return internalError("failed to decode stripe response")
	}
	return nil
}

func (s *Service) updateOrganizationBillingCustomer(ctx context.Context, organizationID string, stripeCustomerID string) error {
	if _, err := s.db.ExecContext(
		ctx,
		`UPDATE organization_billing
			SET stripe_customer_id = ?, updated_at = ?
		WHERE organization_id = ?`,
		strings.TrimSpace(stripeCustomerID),
		s.now().Format(time.RFC3339Nano),
		organizationID,
	); err != nil {
		return internalError("failed to update organization billing customer")
	}
	return nil
}

func (s *Service) ReceiveStripeWebhook(ctx context.Context, payload []byte, signatureHeader string) (StripeWebhookReceipt, error) {
	verificationStatus := "skipped"
	if strings.TrimSpace(s.stripe.WebhookSecret) != "" {
		if err := verifyStripeWebhookSignature(payload, signatureHeader, s.stripe.WebhookSecret, s.now()); err != nil {
			return StripeWebhookReceipt{}, &APIError{Status: 401, Code: "invalid_signature", Message: "stripe signature verification failed"}
		}
		verificationStatus = "verified"
	}

	envelope, err := decodeStripeWebhookEnvelope(payload)
	if err != nil {
		return StripeWebhookReceipt{}, invalidRequest(err.Error())
	}
	receivedAt := s.now().Format(time.RFC3339Nano)
	organizationID := extractStripeOrganizationID(envelope.Data.Object)
	stripeCustomerID := stringValueFromMap(envelope.Data.Object, "customer")
	stripeSubscriptionID := extractStripeSubscriptionID(envelope.Type, envelope.Data.Object)
	stripePriceID := extractStripePriceID(envelope.Data.Object)
	stripeSubscriptionStatus := stringValueFromMap(envelope.Data.Object, "status")
	cancelAtPeriodEnd := optionalBoolValueFromMap(envelope.Data.Object, "cancel_at_period_end")
	currentPeriodEnd := formatStripeTimestamp(envelope.Data.Object["current_period_end"])

	if organizationID == "" {
		resolvedOrganizationID, err := s.resolveOrganizationIDForStripeObjects(ctx, stripeCustomerID, stripeSubscriptionID)
		if err != nil {
			return StripeWebhookReceipt{}, err
		}
		organizationID = resolvedOrganizationID
	}

	eventState, err := s.recordStripeWebhookEvent(
		ctx,
		envelope.ID,
		envelope.Type,
		verificationStatus,
		organizationID,
		stripeCustomerID,
		stripeSubscriptionID,
		receivedAt,
		strings.TrimSpace(signatureHeader),
		payload,
	)
	if err != nil {
		return StripeWebhookReceipt{}, err
	}
	if eventState.ProcessedAt == nil && organizationID != "" {
		if err := s.applyStripeWebhookToBilling(ctx, organizationID, stripeCustomerID, stripeSubscriptionID, stripePriceID, stripeSubscriptionStatus, currentPeriodEnd, cancelAtPeriodEnd, envelope.ID, envelope.Type, receivedAt); err != nil {
			_ = s.markStripeWebhookFailed(ctx, envelope.ID, organizationID, err.Error())
			return StripeWebhookReceipt{}, err
		}
		if err := s.markStripeWebhookProcessed(ctx, envelope.ID, organizationID); err != nil {
			_ = s.markStripeWebhookFailed(ctx, envelope.ID, organizationID, err.Error())
			return StripeWebhookReceipt{}, err
		}
	}

	return StripeWebhookReceipt{
		Received:           true,
		EventID:            envelope.ID,
		EventType:          envelope.Type,
		VerificationStatus: verificationStatus,
		OrganizationID:     organizationID,
	}, nil
}

func (s *Service) billingConfiguration() BillingConfiguration {
	planPriceIDs := map[string]string{}
	for _, planTier := range []string{"starter", "team", "scale", "enterprise"} {
		planPriceIDs[planTier] = s.stripe.PlanPriceIDs[planTier]
	}

	missing := make([]string, 0, 8)
	if s.stripe.SecretKey == "" {
		missing = append(missing, "stripe_secret_key")
	}
	if s.stripe.WebhookSecret == "" {
		missing = append(missing, "stripe_webhook_secret")
	}
	if s.stripe.CheckoutSuccessURL == "" {
		missing = append(missing, "stripe_checkout_success_url")
	}
	if s.stripe.CheckoutCancelURL == "" {
		missing = append(missing, "stripe_checkout_cancel_url")
	}
	if s.stripe.PortalReturnURL == "" {
		missing = append(missing, "stripe_portal_return_url")
	}
	for _, planTier := range []string{"team", "scale"} {
		if strings.TrimSpace(planPriceIDs[planTier]) == "" {
			missing = append(missing, "stripe_price_"+planTier)
		}
	}

	return BillingConfiguration{
		Provider:                 "stripe",
		Configured:               len(missing) == 0,
		PublishableKeyConfigured: s.stripe.PublishableKey != "",
		SecretKeyConfigured:      s.stripe.SecretKey != "",
		WebhookSecretConfigured:  s.stripe.WebhookSecret != "",
		CheckoutConfigured:       s.stripe.SecretKey != "" && s.stripe.CheckoutSuccessURL != "" && s.stripe.CheckoutCancelURL != "" && s.stripe.PlanPriceIDs["team"] != "" && s.stripe.PlanPriceIDs["scale"] != "",
		CustomerPortalConfigured: s.stripe.SecretKey != "" && s.stripe.PortalReturnURL != "",
		PlanPriceIDs:             planPriceIDs,
		MissingConfiguration:     missing,
	}
}

func (s *Service) checkoutMissingConfiguration(planTier string) []string {
	missing := make([]string, 0, 4)
	if s.stripe.SecretKey == "" {
		missing = append(missing, "stripe_secret_key")
	}
	if s.stripe.CheckoutSuccessURL == "" {
		missing = append(missing, "stripe_checkout_success_url")
	}
	if s.stripe.CheckoutCancelURL == "" {
		missing = append(missing, "stripe_checkout_cancel_url")
	}
	if strings.TrimSpace(s.stripe.PlanPriceIDs[planTier]) == "" {
		missing = append(missing, "stripe_price_"+planTier)
	}
	return missing
}

func (s *Service) customerPortalMissingConfiguration(account BillingAccount) []string {
	missing := make([]string, 0, 3)
	if s.stripe.SecretKey == "" {
		missing = append(missing, "stripe_secret_key")
	}
	if s.stripe.PortalReturnURL == "" {
		missing = append(missing, "stripe_portal_return_url")
	}
	if strings.TrimSpace(account.StripeCustomerID) == "" {
		missing = append(missing, "stripe_customer_id")
	}
	return missing
}

func (s *Service) ensureOrganizationBilling(ctx context.Context, organizationID string) (BillingAccount, error) {
	nowText := s.now().Format(time.RFC3339Nano)
	if _, err := s.db.ExecContext(
		ctx,
		`INSERT OR IGNORE INTO organization_billing(
			organization_id, provider, billing_status, stripe_customer_id, stripe_subscription_id, stripe_price_id,
			stripe_subscription_status, current_period_end, cancel_at_period_end, last_webhook_event_id,
			last_webhook_event_type, last_webhook_received_at, created_at, updated_at
		) VALUES (?, 'stripe', 'free', '', '', '', 'not_started', NULL, 0, '', '', NULL, ?, ?)`,
		organizationID,
		nowText,
		nowText,
	); err != nil {
		return BillingAccount{}, internalError("failed to ensure organization billing")
	}
	return s.loadOrganizationBilling(ctx, organizationID)
}

func (s *Service) loadOrganizationBilling(ctx context.Context, organizationID string) (BillingAccount, error) {
	var account BillingAccount
	var currentPeriodEnd sql.NullString
	var lastWebhookReceivedAt sql.NullString
	var cancelAtPeriodEnd int
	err := s.db.QueryRowContext(
		ctx,
		`SELECT organization_id, provider, billing_status, stripe_customer_id, stripe_subscription_id, stripe_price_id,
			stripe_subscription_status, current_period_end, cancel_at_period_end, last_webhook_event_id,
			last_webhook_event_type, last_webhook_received_at, created_at, updated_at
		FROM organization_billing
		WHERE organization_id = ?`,
		organizationID,
	).Scan(
		&account.OrganizationID,
		&account.Provider,
		&account.BillingStatus,
		&account.StripeCustomerID,
		&account.StripeSubscriptionID,
		&account.StripePriceID,
		&account.StripeSubscriptionStatus,
		&currentPeriodEnd,
		&cancelAtPeriodEnd,
		&account.LastWebhookEventID,
		&account.LastWebhookEventType,
		&lastWebhookReceivedAt,
		&account.CreatedAt,
		&account.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return BillingAccount{}, notFound("organization billing not found")
		}
		return BillingAccount{}, internalError("failed to load organization billing")
	}
	account.CurrentPeriodEnd = nullableString(currentPeriodEnd)
	account.CancelAtPeriodEnd = cancelAtPeriodEnd != 0
	account.LastWebhookReceivedAt = nullableString(lastWebhookReceivedAt)
	return account, nil
}

func (s *Service) resolveOrganizationIDForStripeObjects(ctx context.Context, stripeCustomerID string, stripeSubscriptionID string) (string, error) {
	if strings.TrimSpace(stripeCustomerID) != "" {
		var organizationID string
		err := s.db.QueryRowContext(ctx, `SELECT organization_id FROM organization_billing WHERE stripe_customer_id = ?`, stripeCustomerID).Scan(&organizationID)
		if err == nil {
			return organizationID, nil
		}
		if err != nil && err != sql.ErrNoRows {
			return "", internalError("failed to resolve stripe customer billing state")
		}
	}
	if strings.TrimSpace(stripeSubscriptionID) != "" {
		var organizationID string
		err := s.db.QueryRowContext(ctx, `SELECT organization_id FROM organization_billing WHERE stripe_subscription_id = ?`, stripeSubscriptionID).Scan(&organizationID)
		if err == nil {
			return organizationID, nil
		}
		if err != nil && err != sql.ErrNoRows {
			return "", internalError("failed to resolve stripe subscription billing state")
		}
	}
	return "", nil
}

type stripeWebhookEventState struct {
	ProcessedAt *string
}

func (s *Service) recordStripeWebhookEvent(ctx context.Context, eventID string, eventType string, verificationStatus string, organizationID string, stripeCustomerID string, stripeSubscriptionID string, receivedAt string, signatureHeader string, payload []byte) (stripeWebhookEventState, error) {
	if _, err := s.db.ExecContext(
		ctx,
		`INSERT OR IGNORE INTO stripe_webhook_events(
			event_id, event_type, verification_status, organization_id, stripe_customer_id, stripe_subscription_id,
			received_at, processed_at, payload_json, signature_header, processing_error
		) VALUES (?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, '')`,
		eventID,
		eventType,
		verificationStatus,
		organizationID,
		stripeCustomerID,
		stripeSubscriptionID,
		receivedAt,
		string(payload),
		signatureHeader,
	); err != nil {
		return stripeWebhookEventState{}, internalError("failed to record stripe webhook")
	}
	if _, err := s.db.ExecContext(
		ctx,
		`UPDATE stripe_webhook_events
			SET event_type = ?, verification_status = ?, organization_id = CASE WHEN organization_id = '' THEN ? ELSE organization_id END,
				stripe_customer_id = CASE WHEN stripe_customer_id = '' THEN ? ELSE stripe_customer_id END,
				stripe_subscription_id = CASE WHEN stripe_subscription_id = '' THEN ? ELSE stripe_subscription_id END,
				signature_header = CASE WHEN processed_at IS NULL THEN ? ELSE signature_header END,
				payload_json = CASE WHEN processed_at IS NULL THEN ? ELSE payload_json END,
				received_at = CASE WHEN processed_at IS NULL THEN ? ELSE received_at END
		WHERE event_id = ?`,
		eventType,
		verificationStatus,
		organizationID,
		stripeCustomerID,
		stripeSubscriptionID,
		signatureHeader,
		string(payload),
		receivedAt,
		eventID,
	); err != nil {
		return stripeWebhookEventState{}, internalError("failed to refresh stripe webhook state")
	}

	var processedAt sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT processed_at FROM stripe_webhook_events WHERE event_id = ?`, eventID).Scan(&processedAt); err != nil {
		return stripeWebhookEventState{}, internalError("failed to load stripe webhook state")
	}
	return stripeWebhookEventState{ProcessedAt: nullableString(processedAt)}, nil
}

func (s *Service) markStripeWebhookProcessed(ctx context.Context, eventID string, organizationID string) error {
	if _, err := s.db.ExecContext(
		ctx,
		`UPDATE stripe_webhook_events
			SET processed_at = ?, organization_id = CASE WHEN organization_id = '' THEN ? ELSE organization_id END, processing_error = ''
		WHERE event_id = ?`,
		s.now().Format(time.RFC3339Nano),
		organizationID,
		eventID,
	); err != nil {
		return internalError("failed to mark stripe webhook processed")
	}
	return nil
}

func (s *Service) markStripeWebhookFailed(ctx context.Context, eventID string, organizationID string, processingError string) error {
	if _, err := s.db.ExecContext(
		ctx,
		`UPDATE stripe_webhook_events
			SET organization_id = CASE WHEN organization_id = '' THEN ? ELSE organization_id END,
				processing_error = ?, processed_at = NULL
		WHERE event_id = ?`,
		organizationID,
		strings.TrimSpace(processingError),
		eventID,
	); err != nil {
		return internalError("failed to persist stripe webhook processing error")
	}
	return nil
}

func (s *Service) applyStripeWebhookToBilling(ctx context.Context, organizationID string, stripeCustomerID string, stripeSubscriptionID string, stripePriceID string, stripeSubscriptionStatus string, currentPeriodEnd *string, cancelAtPeriodEnd *bool, eventID string, eventType string, receivedAt string) error {
	account, err := s.ensureOrganizationBilling(ctx, organizationID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(stripeCustomerID) != "" {
		account.StripeCustomerID = strings.TrimSpace(stripeCustomerID)
	}
	if strings.TrimSpace(stripeSubscriptionID) != "" {
		account.StripeSubscriptionID = strings.TrimSpace(stripeSubscriptionID)
	}
	if strings.TrimSpace(stripePriceID) != "" {
		account.StripePriceID = strings.TrimSpace(stripePriceID)
	}
	if strings.TrimSpace(stripeSubscriptionStatus) != "" {
		account.StripeSubscriptionStatus = strings.TrimSpace(stripeSubscriptionStatus)
		if billingStatus, ok := billingStatusForStripeSubscription(account.StripeSubscriptionStatus); ok {
			account.BillingStatus = billingStatus
		}
	}
	if currentPeriodEnd != nil {
		account.CurrentPeriodEnd = currentPeriodEnd
	}
	if cancelAtPeriodEnd != nil {
		account.CancelAtPeriodEnd = *cancelAtPeriodEnd
	}
	account.LastWebhookEventID = eventID
	account.LastWebhookEventType = eventType
	account.LastWebhookReceivedAt = &receivedAt
	account.UpdatedAt = receivedAt

	if _, err := s.db.ExecContext(
		ctx,
		`UPDATE organization_billing
			SET stripe_customer_id = ?, stripe_subscription_id = ?, stripe_price_id = ?, stripe_subscription_status = ?,
				billing_status = ?, current_period_end = ?, cancel_at_period_end = ?, last_webhook_event_id = ?,
				last_webhook_event_type = ?, last_webhook_received_at = ?, updated_at = ?
		WHERE organization_id = ?`,
		account.StripeCustomerID,
		account.StripeSubscriptionID,
		account.StripePriceID,
		account.StripeSubscriptionStatus,
		account.BillingStatus,
		nullStringValue(account.CurrentPeriodEnd),
		boolInt(account.CancelAtPeriodEnd),
		account.LastWebhookEventID,
		account.LastWebhookEventType,
		nullStringValue(account.LastWebhookReceivedAt),
		account.UpdatedAt,
		organizationID,
	); err != nil {
		return internalError("failed to update organization billing from stripe webhook")
	}
	if err := s.syncOrganizationPlanTierFromBilling(ctx, organizationID, account.StripePriceID, account.StripeSubscriptionStatus, receivedAt); err != nil {
		return err
	}
	return nil
}

func (s *Service) syncOrganizationPlanTierFromBilling(ctx context.Context, organizationID string, stripePriceID string, stripeSubscriptionStatus string, updatedAt string) error {
	targetPlanTier := s.planTierForStripeSubscription(stripePriceID, stripeSubscriptionStatus)
	if strings.TrimSpace(targetPlanTier) == "" {
		return nil
	}

	var currentPlanTier string
	if err := s.db.QueryRowContext(ctx, `SELECT plan_tier FROM organizations WHERE id = ?`, organizationID).Scan(&currentPlanTier); err != nil {
		if err == sql.ErrNoRows {
			return notFound("organization not found")
		}
		return internalError("failed to load organization billing plan")
	}
	if currentPlanTier == targetPlanTier {
		return nil
	}
	if err := s.applyWorkspaceQuotaConfigForOrganization(ctx, organizationID, targetPlanTier); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE organizations SET plan_tier = ?, updated_at = ? WHERE id = ?`, targetPlanTier, updatedAt, organizationID); err != nil {
		return internalError("failed to sync organization plan tier from stripe")
	}
	return nil
}

func (s *Service) planTierForStripeSubscription(stripePriceID string, stripeSubscriptionStatus string) string {
	status := strings.TrimSpace(stripeSubscriptionStatus)
	switch status {
	case "", "not_started":
		return ""
	case "canceled", "incomplete_expired", "unpaid":
		return "starter"
	}
	for planTier, configuredPriceID := range s.stripe.PlanPriceIDs {
		if strings.TrimSpace(configuredPriceID) == strings.TrimSpace(stripePriceID) && planTier != "starter" {
			return planTier
		}
	}
	return ""
}

func billingStatusForStripeSubscription(subscriptionStatus string) (string, bool) {
	switch strings.TrimSpace(subscriptionStatus) {
	case "trialing", "active", "past_due", "canceled", "incomplete", "incomplete_expired", "unpaid", "paused":
		return strings.TrimSpace(subscriptionStatus), true
	case "free", "not_started":
		return strings.TrimSpace(subscriptionStatus), true
	default:
		return "", false
	}
}

type stripeWebhookEnvelope struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Data struct {
		Object map[string]any `json:"object"`
	} `json:"data"`
}

func decodeStripeWebhookEnvelope(payload []byte) (stripeWebhookEnvelope, error) {
	var envelope stripeWebhookEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return stripeWebhookEnvelope{}, fmt.Errorf("request body must be valid Stripe event JSON")
	}
	if strings.TrimSpace(envelope.ID) == "" {
		return stripeWebhookEnvelope{}, fmt.Errorf("stripe event id is required")
	}
	if strings.TrimSpace(envelope.Type) == "" {
		return stripeWebhookEnvelope{}, fmt.Errorf("stripe event type is required")
	}
	if envelope.Data.Object == nil {
		envelope.Data.Object = map[string]any{}
	}
	return envelope, nil
}

func verifyStripeWebhookSignature(payload []byte, signatureHeader string, secret string, now time.Time) error {
	timestamp, signatures, err := parseStripeSignatureHeader(signatureHeader)
	if err != nil {
		return err
	}
	if timestamp.IsZero() {
		return fmt.Errorf("stripe signature timestamp is required")
	}
	if now.Sub(timestamp) > stripeSignatureTolerance || timestamp.Sub(now) > stripeSignatureTolerance {
		return fmt.Errorf("stripe signature timestamp is outside the allowed tolerance")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(strconv.FormatInt(timestamp.Unix(), 10)))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))
	for _, signature := range signatures {
		if hmac.Equal([]byte(strings.ToLower(expected)), []byte(strings.ToLower(signature))) {
			return nil
		}
	}
	return fmt.Errorf("stripe signature does not match")
}

func parseStripeSignatureHeader(header string) (time.Time, []string, error) {
	parts := strings.Split(strings.TrimSpace(header), ",")
	var timestamp time.Time
	signatures := make([]string, 0, len(parts))
	for _, part := range parts {
		keyValue := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(keyValue) != 2 {
			continue
		}
		switch keyValue[0] {
		case "t":
			unixSeconds, err := strconv.ParseInt(strings.TrimSpace(keyValue[1]), 10, 64)
			if err != nil {
				return time.Time{}, nil, fmt.Errorf("stripe signature timestamp is invalid")
			}
			timestamp = time.Unix(unixSeconds, 0).UTC()
		case "v1":
			signature := strings.TrimSpace(keyValue[1])
			if signature != "" {
				signatures = append(signatures, signature)
			}
		}
	}
	if len(signatures) == 0 {
		return time.Time{}, nil, fmt.Errorf("stripe signature is missing v1 values")
	}
	return timestamp, signatures, nil
}

func extractStripeOrganizationID(object map[string]any) string {
	if metadata := nestedMap(object, "metadata"); metadata != nil {
		if organizationID := stringValueFromMap(metadata, "organization_id"); organizationID != "" {
			return organizationID
		}
	}
	if subscriptionDetails := nestedMap(object, "subscription_details"); subscriptionDetails != nil {
		if metadata := nestedMap(subscriptionDetails, "metadata"); metadata != nil {
			if organizationID := stringValueFromMap(metadata, "organization_id"); organizationID != "" {
				return organizationID
			}
		}
	}
	clientReferenceID := stringValueFromMap(object, "client_reference_id")
	if strings.HasPrefix(clientReferenceID, "org_") {
		return clientReferenceID
	}
	return ""
}

func extractStripeSubscriptionID(eventType string, object map[string]any) string {
	if strings.HasPrefix(eventType, "customer.subscription.") {
		return stringValueFromMap(object, "id")
	}
	if subscriptionID := stringValueFromMap(object, "subscription"); subscriptionID != "" {
		return subscriptionID
	}
	return ""
}

func extractStripePriceID(object map[string]any) string {
	items := nestedMap(object, "items")
	if items != nil {
		if data := nestedSlice(items, "data"); len(data) > 0 {
			if item, ok := data[0].(map[string]any); ok {
				if price := nestedMap(item, "price"); price != nil {
					if priceID := stringValueFromMap(price, "id"); priceID != "" {
						return priceID
					}
				}
			}
		}
	}
	lines := nestedMap(object, "lines")
	if lines != nil {
		if data := nestedSlice(lines, "data"); len(data) > 0 {
			if item, ok := data[0].(map[string]any); ok {
				if price := nestedMap(item, "price"); price != nil {
					if priceID := stringValueFromMap(price, "id"); priceID != "" {
						return priceID
					}
				}
			}
		}
	}
	return ""
}

func formatStripeTimestamp(value any) *string {
	switch typed := value.(type) {
	case float64:
		formatted := time.Unix(int64(typed), 0).UTC().Format(time.RFC3339Nano)
		return &formatted
	case int64:
		formatted := time.Unix(typed, 0).UTC().Format(time.RFC3339Nano)
		return &formatted
	case int:
		formatted := time.Unix(int64(typed), 0).UTC().Format(time.RFC3339Nano)
		return &formatted
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return nil
		}
		if unixSeconds, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
			formatted := time.Unix(unixSeconds, 0).UTC().Format(time.RFC3339Nano)
			return &formatted
		}
		return &trimmed
	default:
		return nil
	}
}

func nestedMap(source map[string]any, key string) map[string]any {
	raw, ok := source[key]
	if !ok {
		return nil
	}
	decoded, _ := raw.(map[string]any)
	return decoded
}

func nestedSlice(source map[string]any, key string) []any {
	raw, ok := source[key]
	if !ok {
		return nil
	}
	decoded, _ := raw.([]any)
	return decoded
}

func stringValueFromMap(source map[string]any, key string) string {
	return strings.TrimSpace(stringValue(source[key]))
}

func boolValueFromAny(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	case float64:
		return typed != 0
	case int:
		return typed != 0
	case int64:
		return typed != 0
	default:
		return false
	}
}

func optionalBoolValueFromMap(source map[string]any, key string) *bool {
	raw, ok := source[key]
	if !ok {
		return nil
	}
	value := boolValueFromAny(raw)
	return &value
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
