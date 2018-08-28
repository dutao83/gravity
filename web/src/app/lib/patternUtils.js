/*
Copyright 2018 Gravitational, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

/*
 *  The MIT License (MIT)
 *  Copyright (c) 2015 Ryan Florence, Michael Jackson
 *  Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:
 *  The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.
 *  THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
*/

import invariant from 'invariant'

function escapeRegExp(string) {
  return string.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

function escapeSource(string) {
  return escapeRegExp(string).replace(/\/+/g, '/+')
}

function _compilePattern(pattern) {
  let regexpSource = '';
  const paramNames = [];
  const tokens = [];

  let match, lastIndex = 0, matcher = /:([a-zA-Z_$][a-zA-Z0-9_$]*)|\*\*|\*|\(|\)/g
  /*eslint no-cond-assign: 0*/
  while ((match = matcher.exec(pattern))) {
    if (match.index !== lastIndex) {
      tokens.push(pattern.slice(lastIndex, match.index))
      regexpSource += escapeSource(pattern.slice(lastIndex, match.index))
    }

    if (match[1]) {
      regexpSource += '([^/?#]+)';
      paramNames.push(match[1]);
    } else if (match[0] === '**') {
      regexpSource += '([\\s\\S]*)'
      paramNames.push('splat');
    } else if (match[0] === '*') {
      regexpSource += '([\\s\\S]*?)'
      paramNames.push('splat');
    } else if (match[0] === '(') {
      regexpSource += '(?:';
    } else if (match[0] === ')') {
      regexpSource += ')?';
    }

    tokens.push(match[0]);

    lastIndex = matcher.lastIndex;
  }

  if (lastIndex !== pattern.length) {
    tokens.push(pattern.slice(lastIndex, pattern.length))
    regexpSource += escapeSource(pattern.slice(lastIndex, pattern.length))
  }

  return {
    pattern,
    regexpSource,
    paramNames,
    tokens
  }
}

const CompiledPatternsCache = {}

export function compilePattern(pattern) {
  if (!(pattern in CompiledPatternsCache))
    CompiledPatternsCache[pattern] = _compilePattern(pattern)

  return CompiledPatternsCache[pattern]
}

/**
 * Attempts to match a pattern on the given pathname. Patterns may use
 * the following special characters:
 *
 * - :paramName     Matches a URL segment up to the next /, ?, or #. The
 *                  captured string is considered a "param"
 * - ()             Wraps a segment of the URL that is optional
 * - *              Consumes (non-greedy) all characters up to the next
 *                  character in the pattern, or to the end of the URL if
 *                  there is none
 * - **             Consumes (greedy) all characters up to the next character
 *                  in the pattern, or to the end of the URL if there is none
 *
 * The return value is an object with the following properties:
 *
 * - remainingPathname
 * - paramNames
 * - paramValues
 */
export function matchPattern(pattern, pathname) {
  // Make leading slashes consistent between pattern and pathname.
  if (pattern.charAt(0) !== '/') {
    pattern = `/${pattern}`
  }
  if (pathname.charAt(0) !== '/') {
    pathname = `/${pathname}`
  }

  let { regexpSource, paramNames, tokens } = compilePattern(pattern)

  regexpSource += '/*' // Capture path separators

  // Special-case patterns like '*' for catch-all routes.
  const captureRemaining = tokens[tokens.length - 1] !== '*'

  if (captureRemaining) {
    // This will match newlines in the remaining path.
    regexpSource += '([\\s\\S]*?)'
  }

  const match = pathname.match(new RegExp('^' + regexpSource + '$', 'i'))

  let remainingPathname, paramValues
  if (match != null) {
    if (captureRemaining) {
      remainingPathname = match.pop()
      const matchedPath =
        match[0].substr(0, match[0].length - remainingPathname.length)

      // If we didn't match the entire pathname, then make sure that the match
      // we did get ends at a path separator (potentially the one we added
      // above at the beginning of the path, if the actual match was empty).
      if (
        remainingPathname &&
        matchedPath.charAt(matchedPath.length - 1) !== '/'
      ) {
        return {
          remainingPathname: null,
          paramNames,
          paramValues: null
        }
      }
    } else {
      // If this matched at all, then the match was the entire pathname.
      remainingPathname = ''
    }

    paramValues = match.slice(1).map(
      v => v != null ? decodeURIComponent(v) : v
    )
  } else {
    remainingPathname = paramValues = null
  }

  return {
    remainingPathname,
    paramNames,
    paramValues
  }
}

export function getParamNames(pattern) {
  return compilePattern(pattern).paramNames
}

export function getParams(pattern, pathname) {
  const { paramNames, paramValues } = matchPattern(pattern, pathname)

  if (paramValues != null) {
    return paramNames.reduce(function (memo, paramName, index) {
      memo[paramName] = paramValues[index]
      return memo
    }, {})
  }

  return null
}

/**
 * Returns a version of the given pattern with params interpolated. Throws
 * if there is a dynamic segment of the pattern for which there is no param.
 */
export function formatPattern(pattern, params) {
  params = params || {}

  const { tokens } = compilePattern(pattern)
  let parenCount = 0, pathname = '', splatIndex = 0

  let token, paramName, paramValue
  for (let i = 0, len = tokens.length; i < len; ++i) {
    token = tokens[i]

    if (token === '*' || token === '**') {
      paramValue = Array.isArray(params.splat) ? params.splat[splatIndex++] : params.splat

      invariant(
        paramValue != null || parenCount > 0,
        'Missing splat #%s for path "%s"',
        splatIndex, pattern
      )

      if (paramValue != null)
        pathname += encodeURI(paramValue)
    } else if (token === '(') {
      parenCount += 1
    } else if (token === ')') {
      parenCount -= 1
    } else if (token.charAt(0) === ':') {
      paramName = token.substring(1)
      paramValue = params[paramName]

      invariant(
        paramValue != null || parenCount > 0,
        'Missing "%s" parameter for path "%s"',
        paramName, pattern
      )

      if (paramValue != null)
        pathname += encodeURIComponent(paramValue)
    } else {
      pathname += token
    }
  }

  return pathname.replace(/\/+/g, '/')
}

export function removeAllOptionalParameters(pattern) {
  return pattern.replace(/\(.*\)/, '');
}