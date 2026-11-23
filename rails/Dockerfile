FROM ruby:3.4-slim AS build

WORKDIR /rails

RUN apt-get update -qq && \
    apt-get install --no-install-recommends -y build-essential libsqlite3-dev libxml2-dev libxslt1-dev libyaml-dev pkg-config zlib1g-dev && \
    rm -rf /var/lib/apt/lists/*

ENV RAILS_ENV=production \
    BUNDLE_DEPLOYMENT=1 \
    BUNDLE_WITHOUT=development:test \
    NOKOGIRI_USE_SYSTEM_LIBRARIES=true

COPY Gemfile Gemfile.lock ./
RUN bundle install && \
    rm -rf /usr/local/bundle/cache /usr/local/bundle/ruby/*/cache

RUN apt-get update -qq && \
    apt-get install --no-install-recommends -y libvips42t64 && \
    rm -rf /var/lib/apt/lists/*

COPY . .
RUN SECRET_KEY_BASE_DUMMY=1 bin/rails assets:precompile

FROM ruby:3.4-slim

WORKDIR /rails

RUN apt-get update -qq && \
    apt-get install --no-install-recommends -y libsqlite3-0 libvips42t64 libxml2 libxslt1.1 libyaml-0-2 && \
    rm -rf /var/lib/apt/lists/* && \
    groupadd --gid 1000 rails && \
    useradd --uid 1000 --gid 1000 --create-home --shell /bin/bash rails

ENV RAILS_ENV=production \
    BUNDLE_DEPLOYMENT=1 \
    BUNDLE_WITHOUT=development:test

COPY --from=build /usr/local/bundle /usr/local/bundle
COPY --from=build /rails /rails

RUN rm -f config/master.key && chown -R rails:rails log storage tmp
USER rails

EXPOSE 3000

CMD ["bin/rails", "server", "-b", "0.0.0.0"]
