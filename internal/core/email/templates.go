package email

import (
	"fmt"
	"strings"
)

// VerificationTemplate composes the verify-your-email message. Centralised
// so we don't end up with three drifted copies in handlers + worker +
// resend client. The link is the public web URL the recipient clicks;
// the API base is implicit (the link points at /verify, the web app
// proxies to /api/v1/auth/verify).
func VerificationTemplate(workspaceName, recipientEmail, verifyURL string) Message {
	subject := fmt.Sprintf("Verify your %s email", brandName(workspaceName))
	html := strings.ReplaceAll(htmlVerifyBody, "{{verifyURL}}", verifyURL)
	html = strings.ReplaceAll(html, "{{workspace}}", workspaceName)
	html = strings.ReplaceAll(html, "{{email}}", recipientEmail)
	text := strings.ReplaceAll(textVerifyBody, "{{verifyURL}}", verifyURL)
	text = strings.ReplaceAll(text, "{{workspace}}", workspaceName)
	return Message{
		To:      []string{recipientEmail},
		Subject: subject,
		HTML:    html,
		Text:    text,
		Tag:     "verify_email",
	}
}

// InvitationTemplate composes the "you've been invited to join X" email.
// inviteURL points at the web app's /accept-invite page; the page POSTs
// the token + a chosen password to /v1/auth/accept-invite which creates
// the user and the tenant membership in one transaction.
func InvitationTemplate(workspaceName, inviterEmail, recipientEmail, inviteURL string) Message {
	subject := fmt.Sprintf("Join %s on Plowered", brandName(workspaceName))
	body := strings.ReplaceAll(htmlInviteBody, "{{inviteURL}}", inviteURL)
	body = strings.ReplaceAll(body, "{{workspace}}", workspaceName)
	body = strings.ReplaceAll(body, "{{email}}", recipientEmail)
	body = strings.ReplaceAll(body, "{{inviter}}", inviterEmail)
	text := strings.ReplaceAll(textInviteBody, "{{inviteURL}}", inviteURL)
	text = strings.ReplaceAll(text, "{{workspace}}", workspaceName)
	text = strings.ReplaceAll(text, "{{inviter}}", inviterEmail)
	return Message{
		To:      []string{recipientEmail},
		Subject: subject,
		HTML:    body,
		Text:    text,
		Tag:     "team_invite",
	}
}

func brandName(s string) string {
	if s == "" {
		return "Plowered"
	}
	return s
}

const textInviteBody = `{{inviter}} invited you to join the {{workspace}} workspace on Plowered.

Accept the invite and set your password here:

{{inviteURL}}

This invitation expires in 7 days.

— The Plowered team
`

const htmlInviteBody = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width" />
<title>Join {{workspace}} on Plowered</title>
</head>
<body style="margin:0;padding:0;background:#FAF6F0;font-family:Segoe UI,system-ui,-apple-system,Roboto,Helvetica,Arial,sans-serif;color:#1F1B17;">
  <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:#FAF6F0;padding:32px 16px;">
    <tr>
      <td align="center">
        <table role="presentation" width="560" cellpadding="0" cellspacing="0" style="background:#ffffff;border:1px solid #E8DDD0;border-radius:8px;overflow:hidden;">
          <tr><td style="padding:28px 32px 8px;">
            <div style="font-size:13px;color:#9D8E7C;letter-spacing:.04em;text-transform:uppercase;font-weight:600;">Plowered · {{workspace}}</div>
          </td></tr>
          <tr><td style="padding:8px 32px 0;">
            <h1 style="font-size:22px;font-weight:600;margin:0 0 8px;">You're invited</h1>
            <p style="font-size:14px;line-height:1.6;color:#3A2F25;margin:0 0 24px;">
              <strong>{{inviter}}</strong> added <strong>{{email}}</strong> to the
              <strong>{{workspace}}</strong> workspace. Accept the invitation and
              pick a password to finish setting up your account.
            </p>
          </td></tr>
          <tr><td style="padding:0 32px 24px;">
            <a href="{{inviteURL}}" style="display:inline-block;background:#B8521B;color:#ffffff;text-decoration:none;font-weight:600;padding:12px 22px;border-radius:4px;font-size:14px;">Accept invite</a>
          </td></tr>
          <tr><td style="padding:0 32px 28px;">
            <p style="font-size:12px;line-height:1.5;color:#9D8E7C;margin:0;">
              Or paste this URL into your browser:
              <br /><span style="font-family:ui-monospace,SFMono-Regular,Menlo,monospace;color:#3A2F25;word-break:break-all;">{{inviteURL}}</span>
            </p>
          </td></tr>
          <tr><td style="padding:16px 32px;border-top:1px solid #E8DDD0;background:#FBF7F1;">
            <p style="font-size:12px;color:#9D8E7C;margin:0;">This invite expires in 7 days. If you weren't expecting it, you can safely ignore this email.</p>
          </td></tr>
        </table>
        <p style="font-size:11px;color:#9D8E7C;margin:16px 0 0;">© Plowered · open data context platform</p>
      </td>
    </tr>
  </table>
</body>
</html>`

const textVerifyBody = `Welcome to Plowered.

Please confirm your email address to finish setting up the {{workspace}} workspace:

{{verifyURL}}

This link expires in 24 hours. If you didn't create a Plowered account,
you can ignore this message.

— The Plowered team
`

// HTML matches the Azure / Microsoft transactional aesthetic: white
// container on neutral-cream, single primary action, no images.
const htmlVerifyBody = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width" />
<title>Verify your email</title>
</head>
<body style="margin:0;padding:0;background:#FAF6F0;font-family:Segoe UI,system-ui,-apple-system,Roboto,Helvetica,Arial,sans-serif;color:#1F1B17;">
  <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:#FAF6F0;padding:32px 16px;">
    <tr>
      <td align="center">
        <table role="presentation" width="560" cellpadding="0" cellspacing="0" style="background:#ffffff;border:1px solid #E8DDD0;border-radius:8px;overflow:hidden;">
          <tr>
            <td style="padding:28px 32px 8px;">
              <div style="font-size:13px;color:#9D8E7C;letter-spacing:.04em;text-transform:uppercase;font-weight:600;">Plowered · {{workspace}}</div>
            </td>
          </tr>
          <tr>
            <td style="padding:8px 32px 0;">
              <h1 style="font-size:22px;font-weight:600;margin:0 0 8px;">Verify your email</h1>
              <p style="font-size:14px;line-height:1.6;color:#3A2F25;margin:0 0 24px;">
                Click the button below to confirm <strong>{{email}}</strong> and finish setting up your workspace. This link expires in 24 hours.
              </p>
            </td>
          </tr>
          <tr>
            <td style="padding:0 32px 24px;">
              <a href="{{verifyURL}}" style="display:inline-block;background:#B8521B;color:#ffffff;text-decoration:none;font-weight:600;padding:12px 22px;border-radius:4px;font-size:14px;">Verify email</a>
            </td>
          </tr>
          <tr>
            <td style="padding:0 32px 28px;">
              <p style="font-size:12px;line-height:1.5;color:#9D8E7C;margin:0;">
                Or paste this URL into your browser:
                <br />
                <span style="font-family:ui-monospace,SFMono-Regular,Menlo,monospace;color:#3A2F25;word-break:break-all;">{{verifyURL}}</span>
              </p>
            </td>
          </tr>
          <tr>
            <td style="padding:16px 32px;border-top:1px solid #E8DDD0;background:#FBF7F1;">
              <p style="font-size:12px;color:#9D8E7C;margin:0;">
                If you didn't create a Plowered account, you can safely ignore this email.
              </p>
            </td>
          </tr>
        </table>
        <p style="font-size:11px;color:#9D8E7C;margin:16px 0 0;">© Plowered · open data context platform</p>
      </td>
    </tr>
  </table>
</body>
</html>`
