# Synthetic City MMDB

`synthetic-city.mmdb` is a minimal, hand-constructed valid MaxMind database for
offline regression tests. It contains no downloaded or production geolocation
data. Querying `1.1.1.1` yields this fabricated record:

- `country.names.en`: `China`; `country.names.zh-CN`: `中国`
- `location.latitude`: double `31.2`
- `location.longitude`: double `121.5`
- `location.accuracy_radius`: **uint16** `10`

The uint16 encoding is intentional: decoding it directly into a floating-point
Go field fails before the rest of the record can be returned. The public JSON
field remains an optional number after conversion. Tests open this checked-in
fixture directly and never depend on `.tools` or external IP services.
