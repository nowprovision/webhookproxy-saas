# WebHookProxy Saas

- No longer maintained

## Introduction

This is the SaaS multi-tenant bootstrapper for the core [webhookproxy](http://www.github.com/nowprovision/webhookproxy) backend library.
It works in conjunction with the database managed by the frontend [webhookproxyweb](http://www.github.com/nowprovision/webhookproxyweb).

This pulls web hook data from a postgres database, monitors for updates, additions and deletions 
so handlers are kept in sync. Gorilla's mux library provides main routing.

This actually powers the free SaaS at https://www.webhookproxy.com, however since this completely 
open source once could easily run on your on cloud or on premise.

Following 12factor principles, environment variables configure the application, see .env.example

## Disclaimer

A lot of stuff isn't optimal, I am well aware. Minimal viable product. 

PR welcomes, contributions will be noted

## License

MIT
