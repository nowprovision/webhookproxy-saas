# WebHookProxy Saas

## Introduction

This is the SaaS multi-tenant bootstrapper for the core [webhookproxy](http://www.github.com/nowprovision/webhookproxy) backend library.
It works in conjunction with the database managed by the frontend [webhookproxyweb](http://www.github.com/nowprovision/webhookproxyweb).

This pulls web hook data from a postgres database, monitors for updates, additions and deletions (the latter two in progress) 
and updates handlers in real-time. 

This powers the free SaaS at https://www.webhookproxy.com but could easily run on-premise if desired. 

Following 12factor, environment variables configure the application, see .env.example

## Disclaimer

A lot of stuff isn't optimal, I am well aware. Minimal viable product and all that malarkey. 

PR welcomes, contributions will be noted

## License

MIT

## Author

Matt Freeman - [@nowprovision](http://www.twitter.com/nowprovision)

