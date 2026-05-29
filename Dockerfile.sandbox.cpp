FROM gcc:13-bookworm
RUN useradd -ms /bin/sh sandbox
RUN mkdir /sandbox && chown sandbox:sandbox /sandbox
WORKDIR /sandbox
USER sandbox
CMD ["sh"]
