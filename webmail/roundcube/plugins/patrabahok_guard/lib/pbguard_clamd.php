<?php

/**
 * patrabahok_guard: minimal clamd client (INSTREAM).
 *
 * Copyright (C) patrabahok contributors
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program. If not, see <https://www.gnu.org/licenses/>.
 */
class pbguard_clamd
{
    const CHUNK = 65536;

    private $uri;
    private $connect_timeout;
    private $scan_timeout;

    public $last_error = '';

    /**
     * @param string $uri unix:///var/run/clamav/clamd.ctl or tcp://host:port
     */
    public function __construct($uri, $connect_timeout = 2, $scan_timeout = 20)
    {
        $this->uri = trim((string) $uri);
        $this->connect_timeout = $connect_timeout;
        $this->scan_timeout = $scan_timeout;
    }

    /** True when the URI is set and (for a Unix socket) the socket exists. */
    public function available()
    {
        if (preg_match('#^unix://(/.+)$#', $this->uri, $m)) {
            return @file_exists($m[1]);
        }

        return (bool) preg_match('#^tcp://[^/:]+:\d+$#', $this->uri);
    }

    /**
     * Scan a file, an open stream or a string.
     *
     * @param string|resource $source  file path, or stream (read from its current position)
     * @param bool            $is_data $source is the data itself
     *
     * @return array ['status' => clean|infected|error, 'virus' => string|null]
     */
    public function scan($source, $is_data = false)
    {
        $this->last_error = '';

        if (!preg_match('#^(unix|tcp)://#', $this->uri)) {
            return $this->fail('invalid clamd URI');
        }

        $errno = 0;
        $errstr = '';
        $sock = @stream_socket_client($this->uri, $errno, $errstr, $this->connect_timeout);
        if (!$sock) {
            return $this->fail("connect failed: {$errstr} ({$errno})");
        }

        stream_set_timeout($sock, $this->scan_timeout);
        $deadline = microtime(true) + $this->scan_timeout;

        $close = false;
        if ($is_data) {
            $fp = fopen('php://temp', 'w+');
            fwrite($fp, (string) $source);
            rewind($fp);
            $close = true;
        } elseif (is_resource($source)) {
            $fp = $source;
        } else {
            $fp = @fopen($source, 'rb');
            if (!$fp) {
                fclose($sock);
                return $this->fail('cannot read file');
            }
            $close = true;
        }

        $ok = $this->write($sock, "zINSTREAM\0");
        while ($ok && !feof($fp)) {
            $chunk = fread($fp, self::CHUNK);
            if ($chunk === false) {
                break;
            }
            if ($chunk === '') {
                continue;
            }
            $ok = $this->write($sock, pack('N', strlen($chunk)) . $chunk);
            if (microtime(true) > $deadline) {
                $ok = false;
            }
        }
        if ($close) {
            fclose($fp);
        }

        if ($ok) {
            $ok = $this->write($sock, pack('N', 0));
        }

        // clamd may answer early (size limit) even when a write failed, so always read.
        $reply = '';
        while (!feof($sock) && microtime(true) < $deadline) {
            $buf = fread($sock, 4096);
            if ($buf === false || $buf === '') {
                $meta = stream_get_meta_data($sock);
                if (!empty($meta['timed_out'])) {
                    break;
                }
                if ($buf === false) {
                    break;
                }
            }
            $reply .= $buf;
            if (strpos($reply, "\0") !== false) {
                break;
            }
        }
        fclose($sock);

        $reply = trim(str_replace("\0", '', $reply));

        if (preg_match('/:\s*(.+?)\s+FOUND$/', $reply, $m)) {
            return ['status' => 'infected', 'virus' => $m[1]];
        }

        if (preg_match('/:\s*OK$/', $reply)) {
            return ['status' => 'clean', 'virus' => null];
        }

        return $this->fail($reply !== '' ? $reply : 'no reply from clamd');
    }

    private function write($sock, $data)
    {
        $len = strlen($data);
        $done = 0;
        while ($done < $len) {
            $n = @fwrite($sock, substr($data, $done));
            if ($n === false || $n === 0) {
                return false;
            }
            $done += $n;
        }

        return true;
    }

    private function fail($msg)
    {
        $this->last_error = $msg;

        return ['status' => 'error', 'virus' => null, 'error' => $msg];
    }
}
