# webos

This contains packages to control a WebOS (LG) TV exposes through an HTTP API. This is mainly designed around using the IP Control protocol, but I've also started on a package that uses SSAP (another control mechanism based on websockets). The terrible magic remote that comes with LG OLEDs has only gotten worse (no play/pause button, select button is a slippery scroll button, et al) so this is being used to replace it with a SofaBaton X1S, as it can program buttons to generate HTTP requests.
