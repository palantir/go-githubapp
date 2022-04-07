# go-githubapp [![GoDoc](https://godoc.org/github.com/palantir/go-githubapp?status.svg)](http://godoc.org/github.com/palantir/go-githubapp)

A library for building [GitHub Apps](https://developer.github.com/apps/) and
other services that handle GitHub webhooks.

The library provides an `http.Handler` implementation that dispatches webhook
events to the correct place, removing boilerplate and letting you focus on the
logic of your application.

* [Usage](#usage)
  + [Examples](#examples)
  + [Dependencies](#dependencies)
* [Asynchronous Dispatch](#asynchronous-dispatch)
* [Structured Logging](#structured-logging)
* [GitHub Clients](#github-clients)
* [Metrics](#metrics)
* [Background Jobs and Multi-Organization Operations](#background-jobs-and-multi-organization-operations)
* [Config Loading](#config-loading)
* [OAuth2](#oauth2)
* [Stability and Versioning Guarantees](#stability-and-versioning-guarantees)
* [Contributing](#contributing)

## Usage

Most users will implement `githubapp.EventHandler` for each webhook
event that needs to be handled. A single implementation can also respond to
multiple event types if they require the same actions:

```go
type CommentHandler struct {
    githubapp.ClientCreator
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

We recommend embedding `githubapp.ClientCreator` in handler implementations as
an easy way to access GitHub clients.

Once you define handlers, register them with an event dispatcher and associate
it with a route in any `net/http`-compatible HTTP router:

```go
func registerRoutes(c githubapp.Config) {
    cc := githubapp.NewDefaultCachingClientCreator(c)

    http.Handle("/api/github/hook", githubapp.NewDefaultEventDispatcher(c,
        &CommentHandler{cc},
        // ...
    ))
}
```

We recommend using [go-baseapp](https://github.com/palantir/go-baseapp) as the minimal server
framework for writing github apps, though go-githubapp works well with the standard library and 
can be easily integrated into most existing frameworks.

### Examples

The [example package](example/main.go) contains a fully functional server
using `go-githubapp`. The example app responds to comments on pull requests by
commenting with a copy of the comment body.

To run the app, update `example/config.yml` with appropriate secrets and then
run:

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

## Asynchronous Dispatch

GitHub imposes timeouts on webhook delivery responses. If an application does
not respond in time, GitHub closes the connection and marks the delivery as
failed. `go-githubapp` optionally supports asynchronous dispatch to help solve
this problem. When enabled, the event dispatcher sends a response to GitHub after
validating the payload and then runs the event handler in a separate goroutine.

To enable, select an appropriate _scheduler_ and configure the event dispatcher
to use it:

```go
dispatcher := githubapp.NewEventDispatcher(handlers, secret, githubapp.WithScheduler(
    githubapp.AsyncScheduler(),
))
```

The following schedulers are included in the library:

- `DefaultScheduler` - a synchronous scheduler that runs event handlers in
  the current goroutine. This is the default mode.

- `AsyncScheduler` - an asynchronous scheduler that handles each event in a
  new goroutine. This is the simplest asynchronous option.

- `QueueAsyncScheduler` - an asynchronous scheduler that queues events and
  handles them with a fixed pool of worker goroutines. This is useful to limit
  the amount of concurrent work.

`AsyncScheduler` and `QueueAsyncScheduler` support several additional options
and customizations; see the documentation for details.

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

`go-githubapp` also exposes various configuration options for GitHub clients.
These are provided when calling `githubapp.NewClientCreator`:

- `githubapp.WithClientUserAgent` sets a `User-Agent` string for all clients
- `githubapp.WithClientTimeout` sets a timeout for requests made by all clients
- `githubapp.WithClientCaching` enables response caching for all v3 (REST) clients.
   The cache can be configured to always validate responses or to respect
   the cache headers returned by GitHub. Re-validation is useful if data
   often changes faster than the requested cache duration.
- `githubapp.WithClientMiddleware` allows customization of the
  `http.RoundTripper` used by all clients and is useful if you want to log
  requests or emit metrics about GitHub requests and responses.

The library provides the following middleware:

- `githubapp.ClientMetrics` emits the standard metrics described below
- `githubapp.ClientLogging` logs metadata about all requests and responses

```go
baseHandler, err := githubapp.NewDefaultCachingClientCreator(
    config.Github,
    githubapp.WithClientUserAgent("example-app/1.0.0"),
    githubapp.WithClientCaching(false, func() httpcache.Cache { return httpcache.NewMemoryCache() }),
    githubapp.WithClientMiddleware(
        githubapp.ClientMetrics(registry),
        githubapp.ClientLogging(zerolog.DebugLevel),
    ),
    ...
)
```

## Metrics

`go-githubapp` uses [rcrowley/go-metrics][] to provide metrics. Metrics are
optional and disabled by default.

GitHub clients emit the following metrics when configured with the
`githubapp.ClientMetrics` middleware:

| metric name | type | definition |
| ----------- | ---- | ---------- |
| `github.requests` | `counter` | the count of successfully completed requests made to GitHub |
| `github.requests.2xx` | `counter` | like `github.requests`, but only counting 2XX status codes |
| `github.requests.3xx` | `counter` | like `github.requests`, but only counting 3XX status codes |
| `github.requests.4xx` | `counter` | like `github.requests`, but only counting 4XX status codes |
| `github.requests.5xx` | `counter` | like `github.requests`, but only counting 5XX status codes |
| `github.requests.cached` | `counter` | the count of successfully cached requests |
| `github.rate.limit[installation:<id>]` | `gauge` | the maximum number of requests permitted to make per hour, tagged with the installation id |
| `github.rate.remaining[installation:<id>]` | `gauge` | the number of requests remaining in the current rate limit window, tagged with the installation id |

When using [asynchronous dispatch](#asynchronous-dispatch), the
`githubapp.WithSchedulingMetrics` option emits the following metrics:

| metric name | type | definition |
| ----------- | ---- | ---------- |
| `github.event.queue` | `gauge` | the number of queued unprocessed event |
| `github.event.workers` | `gauge` | the number of workers actively processing events |
| `github.event.dropped` | `counter` | the number events dropped due to limited queue capacity |
| `github.event.age` | `histogram` | the age (queue time) in milliseconds of events at processing time |

The `MetricsErrorCallback` and `MetricsAsyncErrorCallback` error callbacks for
the event dispatcher and asynchronous schedulers emit the following metrics:

| metric name | type | definition |
| ----------- | ---- | ---------- |
| `github.handler.error[event:<type>]` | `counter` | the number of processing errors, tagged with the GitHub event type |

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
func getOrganizationClient(cc githubapp.ClientCreator, org string) (*github.Client, error) {
    // create a client to perform actions as the application
    appClient, err := cc.NewAppClient()
    if err != nil {
        return nil, err
    }

    // look up the installation ID for a particular organization
    installations := githubapp.NewInstallationsService(appClient)
    install := installations.GetByOwner(context.Background(), org)

    // create a client to perform actions on that specific organization
    return cc.NewInstallationClient(install.ID)
}

```

## Config Loading

The `appconfig` package provides a flexible configuration loader for finding
repository configuration. It supports repository-local files, files containing
remote references, and organization-level defaults.

By default, the loader will:

1. Try a list of paths in the repository
2. If a file exists at a path, load its contents
3. If the contents define a remote reference, load the remote file. Otherwise,
   return the contents.
4. If no files exist in the repository, try a list of paths in a `.github`
   repository owned by the same owner.

Users can customize the paths, the remote reference encoding, whether remote
references are enabled, the name of the owner-level default repository, and
whether the owner-level default is enabled.

The standard remote reference encoding is YAML:

```yaml
remote: owner/repo
path: config/app.yml
ref: develop
```

Usage is straightforward:

```go
func loadConfig(ctx context.Context, client *github.Client, owner, repo, ref string) (*AppConfig, error) {
    loader := appconfig.NewLoader([]string{".github/app.yml"})

    c, err := loader.LoadConfig(ctx, client, onwer, repo, ref)
    if err != nil {
        return nil, err
    }
    if c.IsUndefined() {
        return nil, nil
    }

    var appConfig AppConfig
    if err := yaml.Unmarshal(c.Content, &appConfig); err != nil {
        return nil, err
    }
    return &appConfig, nil
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

## Customizing Webhook Responses

For most applications, the default responses should be sufficient: they use
correct status codes and include enough information to match up GitHub delivery
records with request logs. If your application has additional requirements for
responses, two methods are provided for customization:

- Error responses can be modified with a custom error callback. Use the
  `WithErrorCallback` option when creating an event dispatcher.

- Non-error responses can be modified with a custom response callback. Use the
  `WithResponseCallback` option when creating an event dispatcher.

- Individual hook responses can be modified by calling the `SetResponder`
  function before the handler returns. Note that if you register a custom
  response handler as described above, you must make it aware of handler-level
  responders if you want to keep using `SetResponder`. See the default response
  callback for an example of how to implement this.

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
