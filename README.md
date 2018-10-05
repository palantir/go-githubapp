# go-githubapp [![GoDoc](https://godoc.org/github.com/palantir/go-githubapp?status.svg)](http://godoc.org/github.com/palantir/go-githubapp)

A library for building [GitHub Apps](https://developer.github.com/apps/) and
other services that handle GitHub webhooks.

The library provides an `http.Handler` implementation that dispatches webhook
events to the correct place, removing boilerplate and letting you focus on the
logic of your application.

* [Usage](#usage)
  + [Examples](#examples)
  + [Dependencies](#dependencies)
* [Structured Logging](#structured-logging)
* [GitHub Clients](#github-clients)
  + [Metrics](#metrics)
* [Background Jobs and Multi-Organization Operations](#background-jobs-and-multi-organization-operations)
* [OAuth2](#oauth2)
* [Stability and Versioning Guarantees](#stability-and-versioning-guarantees)
* [Contributing](#contributing)

## Usage

Most users will implement `githubapp.EventHandler` for each webhook
event that needs to be handled. A single implementation can also respond to
multiple event types if they require the same actions:

```go
type CommentHandler struct {
    githubapp.BaseHandler
}

func (h *CommentHandler) Handles() []string {
    return []string{"issue_comment"}
}

func (h *CommentHandler) Handle(ctx context.Context, eventType, deliveryID string, payload []byte) error {
    // from github.com/google/go-github/github
    var event github.IssueCommentEvent
    if err := json.Unmarshal(payload, &event); err != nil {
        return err
    }

    // do something with the content of the event
}
```

We recommend embedding `githubapp.BaseHandler` in handler implementations. It
provides access to GitHub clients and other utility methods.

Once you define handlers, register them with an event dispatcher and associate
it with a route in any `net/http`-compatible HTTP router:

```go
func registerRoutes(c githubapp.Config) {
    base := githubapp.NewDefaultBaseHandler(c)

    http.Handle("/api/github/hook", githubapp.NewDefaultEventDispatcher(c,
        &CommentHandler{base},
        // ...
    ))
}
```

### Examples

The [example package](example/main.go) contains a fully functional server
using `go-githubapp`. The example app responds to comments on pull requests by
commenting with a copy of the comment body.

To run the app, update `example/config.yml` with appropriate secrets and then
run:

    ./godelw dep
    ./godelw run example

### Dependencies

`go-githubapp` has minimal dependencies, but does make some decisions:

- [rs/zerolog](https://github.com/rs/zerolog) for logging
- [rcrowley/go-metrics](https://github.com/rcrowley/go-metrics) for metrics
- [google/go-github](https://github.com/google/go-github) for v3 (REST) API client
- [shurcooL/githubv4](https://github.com/shurcooL/githubv4) for v4 (GraphQL) API client

Logging and metrics are only active when they are configured (see below). This
means you can add your own logging or metrics libraries without conflict, but
will miss out on the free built-in support.

## Structured Logging

`go-githubapp` uses [rs/zerolog](https://github.com/rs/zerolog) for structured
logging. A logger must be stored in the `context.Context` associated with each
`http.Request`:

```
func (d *EventDispatcher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    logger := zerolog.Ctx(r.Context())
    ...
}
```

If there is no logger in the context, log output is disabled. It's expected
that HTTP middleware, like that provided by the [hlog package][], configures
the `http.Request` context automatically.

Below are the standard keys used when logging events. They are also exported as
constants.

| exported constant | key | definition |
| ----------------- | --- | ---------- |
| `LogKeyEventType` | `github_event_type` | the [github event type header](https://developer.github.com/webhooks/#delivery-headers) |
| `LogKeyDeliveryID` | `github_delivery_id` | the [github event delivery id header](https://developer.github.com/webhooks/#delivery-headers) |
| `LogKeyInstallationID` | `github_installation_id` | the [installation id the app is authenticating with](https://developer.github.com/apps/building-github-apps/authenticating-with-github-apps/#accessing-api-endpoints-as-a-github-app) |
| `LogKeyRepositoryName` | `github_repository_name` | the repository name of the pull request being acted on |
| `LogKeyRepositoryOwner` | `github_repository_owner` | the repository owner of the pull request being acted on |
| `LogKeyPRNum` | `github_pr_num` | the number of the pull request being acted on |

Where appropriate, the library creates derived loggers with the above keys set
to the correct values.

[hlog package]: https://github.com/rs/zerolog#integration-with-nethttp

## GitHub Clients

Authenticated and configured GitHub clients can be retrieved from
`githubapp.ClientCreator` implementations. The library provides a basic
implementation and a caching version.

There are three types of clients and two API versions for a total of six
distinct clients:

- An _application_ client authenticates [as the application][] and can only
  call limited APIs that mostly return application metadata.
- An _installation_ client authenticates [as an installation][] and can call
  any APIs where the has been installed and granted permissions.
- A _token_ client authenticates with a static OAuth2 token associated with a
  user account.

[as the application]: https://developer.github.com/apps/building-github-apps/authenticating-with-github-apps/#authenticating-as-a-github-app
[as an installation]: https://developer.github.com/apps/building-github-apps/authenticating-with-github-apps/#authenticating-as-an-installation

The `githubapp.BaseHandler` type embeds a `ClientCreator` so event handlers
have easy access to GitHub clients.

`go-githubapp` also exposes various configuration options for GitHub clients.
These are provided when calling `githubapp.NewClientCreator` and through the
`githubapp.NewDefaultBaseHandler` convenience function:

- `githubapp.WithClientUserAgent` sets a `User-Agent` string for all clients
- `githubapp.WithClientMiddleware` allows customization of the
  `http.RoundTripper` used by all clients and is useful if you want to log
  requests or emit metrics about GitHub requests and responses.

Add the built-in `githubapp.ClientMetrics` middleware to emit the standard
metrics described below.

```go
baseHandler, err := githubapp.NewDefaultBaseHandler(
    config.Github,
    githubapp.WithClientUserAgent("example-app/1.0.0"),
    githubapp.WithClientMiddleware(
        githubapp.ClientMetrics(registry),
    ),
    ...
)
```

### Metrics

`go-githubapp` uses [rcrowley/go-metrics][] to provide metrics. GitHub clients
emit the metrics below if configured with the `githubapp.ClientMetrics`
middleware.

| metric name | type | definition |
| ----------- | ---- | ---------- |
| `github.requests` | `counter` | the count of successfully completed requests made to GitHub |
| `github.requests.2xx` | `counter` | like `github.requests`, but only counting 2XX status codes |
| `github.requests.3xx` | `counter` | like `github.requests`, but only counting 3XX status codes |
| `github.requests.4xx` | `counter` | like `github.requests`, but only counting 4XX status codes |
| `github.requests.5xx` | `counter` | like `github.requests`, but only counting 5XX status codes |

Note that metrics need to be published in order to be useful. Several
[publishing options][] are available or you can implement your own.

[rcrowley/go-metrics]: https://github.com/rcrowley/go-metrics
[publishing options]: https://github.com/rcrowley/go-metrics#publishing-metrics

## Background Jobs and Multi-Organization Operations

While applications will mostly operate on the installation IDs provided in
webhook payloads, sometimes there is a need to run background jobs or make API
calls against multiple organizations. In these cases, use an _application
client_ to look up specific installations of the application and then construct
an _installation client_ to make API calls:

```go
func getOrganizationClient(cc githubapp.ClientCreator, org name) (*github.Client, error) {
    // create a client to perform actions as the application
    appClient, err := cc.NewAppClient()
    if err != nil {
        return nil, err
    }

    // look up the installation ID for a particular organization
    installClient := githubapp.NewInstallationClient(app)
    install := installClient.GetByOwner(context.Background(), org)

    // create a client to perform actions on that specific organization
    return cc.NewInstallationClient(install.ID)
}

```

## OAuth2

The `oauth2` package provides an `http.Handler` implementation that simplifies
OAuth2 authentication with GitHub. When a user visits the endpoint, they are
redirected to GitHub to authorize the application. GitHub redirects back to the
same endpoint, which performs the code exchange and obtains a token for the
user. The token is passed to a callback for further processing.

```go
func registerOAuth2Handler(c githubapp.Config) {
    http.Handle("/api/auth/github", oauth2.NewHandler(
        oauth2.GetConfig(c, []string{"user:email"}),
        // force generated URLs to use HTTPS; useful if the app is behind a reverse proxy
        oauth2.ForceTLS(true),
        // set the callback for successful logins
        oauth2.OnLogin(func(w http.ResponseWriter, r *http.Request, login *oauth2.Login) {
            // look up the current user with the authenticated client
            client := github.NewClient(login.Client)
            user, _, err := client.Users.Get(r.Context(), "")
            // handle error, save the user, ...

            // redirect the user back to another page
            http.Redirect(w, r, "/dashboard", http.StatusFound)
        }),
    ))
}
```

Production applications should also use the `oauth2.WithStore` option to set a
secure `StateStore` implementation. `oauth2.SessionStateStore` is a good choice
that uses [alexedwards/scs](https://github.com/alexedwards/scs) to store the
state in a session.

## Stability and Versioning Guarantees

While we've used this library to build multiple applications internally,
there's still room for API tweaks and improvements as we find better ways to
solve problems. These will be backwards compatible when possible and should
require only minor changes when not.

Releases will be tagged periodically and will follow semantic versioning, with
new major versions tagged after any backwards-incompatible changes. Still, we
recommend vendoring this library to avoid surprises.

In general, fixes will only be applied to trunk and future releases, not
backported to older versions.

## Contributing

Contributions and issues are welcome. For new features or large contributions,
we prefer discussing the proposed change on a GitHub issue prior to a PR.

New functionality should avoid adding new dependencies if possible and should
be broadly useful. Feature requests that are specific to certain uses will
likely be declined unless they can be redesigned to be generic or optional.

Before submitting a pull request, please run tests and style checks:

```
./godelw verify
```

## License

This library is made available under the [Apache 2.0 License](http://www.apache.org/licenses/LICENSE-2.0).
