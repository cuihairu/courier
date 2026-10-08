// L3 Adapter:令牌安全存储(layers.md L3「存储」——非明文 PlayerPrefs)。
// AES-256-CBC + HMAC-SHA256(encrypt-then-MAC),密钥经 PBKDF2-SHA256 从
// 设备标识 + gameId 派生,密文 base64 落 PlayerPrefs。
// 威胁模型:静态混淆级(防明文落盘);硬件级安全(Keychain/Keystore)经 ITokenStore
// 换实现即可,L2 不变。
using System;
using System.Security.Cryptography;
using System.Text;
using Courier.Core;
using UnityEngine;

namespace Courier.Adapter
{
    public sealed class SecureTokenStore : ITokenStore
    {
        const int Iterations = 10000;
        const string PrefsKeyPrefix = "courier.tokens.";
        static readonly byte[] SaltPrefix = Encoding.UTF8.GetBytes("courier.v1|");

        readonly string _prefsKey;
        readonly byte[] _encKey;
        readonly byte[] _macKey;

        public SecureTokenStore(string gameId, string deviceId)
        {
            _prefsKey = PrefsKeyPrefix + gameId;
            using (var derive = new Rfc2898DeriveBytes(
                Encoding.UTF8.GetBytes(deviceId),
                Concat(SaltPrefix, Encoding.UTF8.GetBytes(gameId)),
                Iterations,
                HashAlgorithmName.SHA256))
            {
                _encKey = derive.GetBytes(32);
                _macKey = derive.GetBytes(32);
            }
        }

        public StoredTokens Load()
        {
            var payload = PlayerPrefs.GetString(_prefsKey, null);
            if (string.IsNullOrEmpty(payload))
            {
                return null;
            }
            try
            {
                return Json.Deserialize<StoredTokens>(Decrypt(payload));
            }
            catch (Exception)
            {
                // 不可解(换设备/换密钥/被篡改):按无会话处理并清残骸,让 L2 走重新登录。
                Clear();
                return null;
            }
        }

        public void Save(StoredTokens tokens)
        {
            if (tokens == null)
            {
                Clear();
                return;
            }
            PlayerPrefs.SetString(_prefsKey, Encrypt(Json.Serialize(tokens)));
            PlayerPrefs.Save();
        }

        public void Clear()
        {
            PlayerPrefs.DeleteKey(_prefsKey);
            PlayerPrefs.Save();
        }

        // 格式:base64(iv(16) || ciphertext || hmac(32))。
        string Encrypt(string plaintext)
        {
            var iv = new byte[16];
            using (var rng = RandomNumberGenerator.Create())
            {
                rng.GetBytes(iv);
            }
            byte[] ciphertext;
            using (var aes = Aes.Create())
            {
                aes.Key = _encKey;
                aes.IV = iv;
                aes.Mode = CipherMode.CBC;
                aes.Padding = PaddingMode.PKCS7;
                using (var encryptor = aes.CreateEncryptor())
                {
                    var plain = Encoding.UTF8.GetBytes(plaintext);
                    ciphertext = encryptor.TransformFinalBlock(plain, 0, plain.Length);
                }
            }
            var mac = ComputeMac(iv, ciphertext);

            var payload = new byte[iv.Length + ciphertext.Length + mac.Length];
            Buffer.BlockCopy(iv, 0, payload, 0, iv.Length);
            Buffer.BlockCopy(ciphertext, 0, payload, iv.Length, ciphertext.Length);
            Buffer.BlockCopy(mac, 0, payload, iv.Length + ciphertext.Length, mac.Length);
            return Convert.ToBase64String(payload);
        }

        string Decrypt(string payload)
        {
            var data = Convert.FromBase64String(payload);
            if (data.Length < 16 + 32)
            {
                throw new CryptographicException("payload too short");
            }
            var iv = new byte[16];
            var mac = new byte[32];
            var ciphertext = new byte[data.Length - iv.Length - mac.Length];
            Buffer.BlockCopy(data, 0, iv, 0, iv.Length);
            Buffer.BlockCopy(data, iv.Length, ciphertext, 0, ciphertext.Length);
            Buffer.BlockCopy(data, iv.Length + ciphertext.Length, mac, 0, mac.Length);

            // 先验 MAC(encrypt-then-MAC):密文不可信前不进解密器。
            var expected = ComputeMac(iv, ciphertext);
            if (!FixedTimeEquals(expected, mac))
            {
                throw new CryptographicException("mac mismatch");
            }

            using (var aes = Aes.Create())
            {
                aes.Key = _encKey;
                aes.IV = iv;
                aes.Mode = CipherMode.CBC;
                aes.Padding = PaddingMode.PKCS7;
                using (var decryptor = aes.CreateDecryptor())
                {
                    return Encoding.UTF8.GetString(
                        decryptor.TransformFinalBlock(ciphertext, 0, ciphertext.Length));
                }
            }
        }

        byte[] ComputeMac(byte[] iv, byte[] ciphertext)
        {
            using (var hmac = new HMACSHA256(_macKey))
            {
                return hmac.ComputeHash(Concat(iv, ciphertext));
            }
        }

        static byte[] Concat(byte[] a, byte[] b)
        {
            var merged = new byte[a.Length + b.Length];
            Buffer.BlockCopy(a, 0, merged, 0, a.Length);
            Buffer.BlockCopy(b, 0, merged, a.Length, b.Length);
            return merged;
        }

        static bool FixedTimeEquals(byte[] a, byte[] b)
        {
            if (a.Length != b.Length)
            {
                return false;
            }
            var diff = 0;
            for (int i = 0; i < a.Length; i++)
            {
                diff |= a[i] ^ b[i];
            }
            return diff == 0;
        }
    }
}
